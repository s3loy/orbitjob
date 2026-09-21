//go:build integration

package http

import (
	"net/http"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/admin/bootstrap"
	domainworkflow "orbitjob/internal/core/domain/workflow"
)

// workflowRunGVR is the resource identity the workflow routes publish and
// patch under.
var workflowRunGVR = v1alpha1.GroupVersion.WithResource("workflowruns")

func workflowIntegrationDef() WorkflowDefinition {
	return WorkflowDefinition{
		ID: 5, Name: "nightly-dag", Namespace: "orbitjob",
		SourceUID: "wf-uid-nightly", Generation: 2,
		Spec: v1alpha1.WorkflowJobSpec{
			Schedule: "@daily",
			Tasks: []v1alpha1.WorkflowTaskSpec{
				{Name: "extract", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "extract-job"}},
				{Name: "load", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "load-job"}, DependsOn: []string{"extract"}},
			},
		},
	}
}

// TestWorkflowTrigger_PublishesWorkflowRunCR drives the manual trigger through
// the real auth stack and the dynamic fake: the WorkflowRun custom resource
// must land in the cluster with the Manual trigger, the caller's key as its
// actor, and the pinned revision -- and a replayed trigger with the same
// idempotency key must resolve to the same object with a 200.
func TestWorkflowTrigger_PublishesWorkflowRunCR(t *testing.T) {
	server, _, key, stores := newIntegrationServerWithWorkloads(t)
	defer server.Close()

	stores.workflowDefs.seed(workflowIntegrationDef())

	resp := postJSONHeader(t, server, server.URL+"/api/v1/workflows/5/runs", key,
		map[string]string{"X-OrbitJob-Idempotency-Key": "wf-idem-1"}, nil)
	requireStatus(t, resp, http.StatusCreated)
	var out struct {
		Namespace     string `json:"namespace"`
		Name          string `json:"name"`
		OccurrenceKey string `json:"occurrence_key"`
		Trigger       string `json:"trigger"`
		Created       bool   `json:"created"`
	}
	decodeJSON(t, resp, &out)

	if !out.Created || out.Trigger != "Manual" {
		t.Fatalf("unexpected trigger result: %+v", out)
	}
	if !strings.HasPrefix(out.Name, "nightly-dag-") {
		t.Fatalf("name = %q, want the WorkflowRunObjectName derivation", out.Name)
	}

	object, err := stores.tracker().Get(workflowRunGVR, out.Namespace, out.Name)
	if err != nil {
		t.Fatalf("read published WorkflowRun: %v", err)
	}
	var run v1alpha1.WorkflowRun
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(
		object.(*unstructured.Unstructured).Object, &run); err != nil {
		t.Fatalf("decode published WorkflowRun: %v", err)
	}
	if run.Spec.Trigger != v1alpha1.Manual {
		t.Errorf("spec.trigger = %q, want %q", run.Spec.Trigger, v1alpha1.Manual)
	}
	if run.Spec.Actor == "" {
		t.Error("spec.actor is empty; the workflow run would not answer who triggered it")
	}
	if run.Spec.WorkflowRef.Name != "nightly-dag" || run.Spec.WorkflowRef.UID != "wf-uid-nightly" {
		t.Errorf("spec.workflowRef = %+v, want the definition's identity", run.Spec.WorkflowRef)
	}
	if run.Spec.DefinitionRevision != 5 {
		t.Errorf("spec.definitionRevision = %d, want the active revision 5", run.Spec.DefinitionRevision)
	}

	// The replay resolves to the object that already exists.
	replay := postJSONHeader(t, server, server.URL+"/api/v1/workflows/5/runs", key,
		map[string]string{"X-OrbitJob-Idempotency-Key": "wf-idem-1"}, nil)
	requireStatus(t, replay, http.StatusOK)
	var replayOut struct {
		Name    string `json:"name"`
		Created bool   `json:"created"`
	}
	decodeJSON(t, replay, &replayOut)
	if replayOut.Created {
		t.Fatal("a replayed trigger must not report created=true")
	}
	if replayOut.Name != out.Name {
		t.Fatalf("replay created %s, want the first trigger's %s", replayOut.Name, out.Name)
	}
}

// Triggering a suspended workflow is a conflict: the definition exists and the
// caller reached it, but the platform has been told to hold its fire.
func TestWorkflowTrigger_SuspendedIsConflict(t *testing.T) {
	server, _, key, stores := newIntegrationServerWithWorkloads(t)
	defer server.Close()

	def := workflowIntegrationDef()
	def.Spec.Suspend = true
	stores.workflowDefs.seed(def)

	resp := postJSON(t, server.Client(), server.URL+"/api/v1/workflows/5/runs", key, nil)
	requireStatus(t, resp, http.StatusConflict)
}

// seedWorkflowRunCR places a WorkflowRun custom resource in the fake cluster
// under the name the cancel route will derive.
func seedWorkflowRunCR(t *testing.T, stores *workloadsTestStores, def WorkflowDefinition, occurrenceKey string) {
	t.Helper()
	run := v1alpha1.WorkflowRun{
		TypeMeta: v1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "WorkflowRun"},
		ObjectMeta: v1.ObjectMeta{
			Namespace: def.Namespace,
			Name:      v1alpha1.WorkflowRunObjectName(def.Name, occurrenceKey),
		},
		Spec: v1alpha1.WorkflowRunSpec{
			WorkflowRef:        v1alpha1.ObjectReference{Name: def.Name, UID: def.SourceUID},
			DefinitionRevision: def.ID,
			Trigger:            v1alpha1.Manual,
			Actor:              "key-test",
			OccurrenceKey:      occurrenceKey,
		},
	}
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&run)
	if err != nil {
		t.Fatalf("encode seed WorkflowRun: %v", err)
	}
	if err := stores.tracker().Add(&unstructured.Unstructured{Object: object}); err != nil {
		t.Fatalf("add seed WorkflowRun: %v", err)
	}
}

// Cancel patches cancelRequested onto the WorkflowRun custom resource: the
// operator fans the stop out over the non-terminal step runs. The ledger row
// stays at the phase observed at request time, and the patch is visible on
// the object in the cluster.
func TestWorkflowCancel_PatchesCancelRequested(t *testing.T) {
	server, _, key, stores := newIntegrationServerWithWorkloads(t)
	defer server.Close()

	def := workflowIntegrationDef()
	stores.workflowDefs.seed(def)
	stores.workflowRuns.runs = map[int64]domainworkflow.Run{
		12: {ID: 12, TenantID: bootstrap.DefaultTenantID, SourceUID: def.SourceUID,
			OccurrenceKey: "key-12", Trigger: "Manual", Phase: domainworkflow.PhaseRunning},
	}
	seedWorkflowRunCR(t, stores, def, "key-12")

	resp := postJSON(t, server.Client(), server.URL+"/api/v1/workflows/5/runs/12/cancel", key, nil)
	requireStatus(t, resp, http.StatusOK)
	var out struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
		Phase     string `json:"phase"`
	}
	decodeJSON(t, resp, &out)
	if out.Phase != "Running" {
		t.Fatalf("phase = %q, want the phase observed at request time", out.Phase)
	}

	object, err := stores.tracker().Get(workflowRunGVR, "orbitjob", out.Name)
	if err != nil {
		t.Fatalf("read patched WorkflowRun: %v", err)
	}
	requested, found, err := unstructured.NestedBool(object.(*unstructured.Unstructured).Object, "spec", "cancelRequested")
	if err != nil || !found || !requested {
		t.Fatalf("spec.cancelRequested not patched: found=%v value=%v err=%v", found, requested, err)
	}
}

// A workflow that already reached a terminal phase succeeds without needing
// the custom resource to exist: a finished workflow is final, and the current
// phase is the answer.
func TestWorkflowCancel_TerminalRunNeedsNoResource(t *testing.T) {
	server, _, key, stores := newIntegrationServerWithWorkloads(t)
	defer server.Close()

	def := workflowIntegrationDef()
	stores.workflowDefs.seed(def)
	stores.workflowRuns.runs = map[int64]domainworkflow.Run{
		12: {ID: 12, TenantID: bootstrap.DefaultTenantID, SourceUID: def.SourceUID,
			OccurrenceKey: "key-12", Trigger: "Manual", Phase: domainworkflow.PhaseSucceeded},
	}

	resp := postJSON(t, server.Client(), server.URL+"/api/v1/workflows/5/runs/12/cancel", key, nil)
	requireStatus(t, resp, http.StatusOK)
	var out struct {
		Phase string `json:"phase"`
	}
	decodeJSON(t, resp, &out)
	if out.Phase != "Succeeded" {
		t.Fatalf("phase = %q, want the terminal phase", out.Phase)
	}
}

// Ledger-open but cluster-absent -- the custom resource was lost or pruned
// while the row is not yet terminal -- is a conflict, not a 404: the ledger
// row exists, and 404 would claim otherwise.
func TestWorkflowCancel_MissingCRIsConflict(t *testing.T) {
	server, _, key, stores := newIntegrationServerWithWorkloads(t)
	defer server.Close()

	def := workflowIntegrationDef()
	stores.workflowDefs.seed(def)
	stores.workflowRuns.runs = map[int64]domainworkflow.Run{
		12: {ID: 12, TenantID: bootstrap.DefaultTenantID, SourceUID: def.SourceUID,
			OccurrenceKey: "key-12", Trigger: "Manual", Phase: domainworkflow.PhaseRunning},
	}

	resp := postJSON(t, server.Client(), server.URL+"/api/v1/workflows/5/runs/12/cancel", key, nil)
	requireStatus(t, resp, http.StatusConflict)
}

// A run of a different workflow is not found under this workflow's prefix.
func TestWorkflowGetRun_ForeignRunIsNotFound(t *testing.T) {
	server, _, key, stores := newIntegrationServerWithWorkloads(t)
	defer server.Close()

	stores.workflowDefs.seed(workflowIntegrationDef())
	stores.workflowRuns.runs = map[int64]domainworkflow.Run{
		13: {ID: 13, TenantID: bootstrap.DefaultTenantID, SourceUID: "wf-uid-other",
			OccurrenceKey: "key-13", Trigger: "Schedule", Phase: domainworkflow.PhaseRunning},
	}

	resp := get(t, server.Client(), server.URL+"/api/v1/workflows/5/runs/13", key)
	requireStatus(t, resp, http.StatusNotFound)
}

// A group-scoped key cannot reach workflow definitions: a workflow revision
// has no resource group to be limited by, so serving it would widen the
// grant, and the refusal is 403.
func TestWorkflowList_GroupScopedKeyForbidden(t *testing.T) {
	server, db, key, stores := newIntegrationServerWithWorkloads(t)
	defer server.Close()
	_ = db

	stores.workflowDefs.seed(workflowIntegrationDef())

	// A group to scope the key to.
	resp := postJSON(t, server.Client(), server.URL+"/api/v1/resource_groups", key, map[string]any{
		"slug": "ci",
		"name": "Continuous Integration",
	})
	requireStatus(t, resp, http.StatusCreated)
	var group struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &group)

	// The scoped key carries AdministratorAccess on purpose: without the
	// scope guard the wildcard grant would let it through, so the 403 below
	// can only be the scope refusal, not a missing grant.
	scoped := postJSON(t, server.Client(),
		server.URL+"/api/v1/tenants/"+bootstrap.DefaultTenantID+"/api_keys", key,
		map[string]any{
			"policies":          []string{administratorPolicyID},
			"resource_group_id": group.ID,
		})
	requireStatus(t, scoped, http.StatusCreated)
	var scopedKey struct {
		Key string `json:"key"`
	}
	decodeJSON(t, scoped, &scopedKey)

	denial := get(t, server.Client(), server.URL+"/api/v1/workflows", scopedKey.Key)
	requireStatus(t, denial, http.StatusForbidden)
}

// The deferred end-to-end case from the policy-GET repair: a missing policy id
// answers 404 through the real router and store, not a 500.
func TestGetPolicy_MissingIDReturns404(t *testing.T) {
	server, _, key, _ := newIntegrationServerWithWorkloads(t)
	defer server.Close()

	resp := get(t, server.Client(), server.URL+"/api/v1/policies/01JBB0W9YRXG4SZV2QKM78N3ZZ", key)
	requireStatus(t, resp, http.StatusNotFound)
}

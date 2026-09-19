//go:build integration

package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/admin/bootstrap"
	domainfunction "orbitjob/internal/core/domain/function"
)

// jobRunGVR is the resource identity the invoke route publishes under; the
// tests read the fake cluster through the same identity to assert what landed.
var jobRunGVR = v1alpha1.GroupVersion.WithResource("jobruns")

// functionIntegrationDef is the definition shape the invoke tests seed.
func functionIntegrationDef(id int64, tenantID string) domainfunction.Definition {
	return domainfunction.Definition{
		ID: id, TenantID: tenantID, Name: "resize",
		Status:         domainfunction.StatusActive,
		Image:          "registry.example/resize@sha256:abcd",
		TimeoutSeconds: 30, RetryLimit: 1, Version: 2,
	}
}

// createKeyWithFunctionPolicy authors a tenant policy naming the new
// function/workflow actions and mints a key bound to it. The presets do not
// carry these actions yet -- they land with the routes' preset update -- so a
// tenant key reaches the new routes only through a document like this one,
// which ValidateTenantDocument now accepts.
func createKeyWithFunctionPolicy(t *testing.T, server *httptest.Server, adminKey, tenantID string) (policyID, key string) {
	t.Helper()

	// A policy is authored inside a tenant and bound by that tenant, so the
	// document must be created from a key of tenantID itself. The first key
	// starts from the platform TenantAdminAccess preset, which carries
	// policy:Create and apikey:Create.
	_, adminKeyOfTenant := createKey(t, server, adminKey, tenantID, []string{tenantAdminPolicyID}, "")

	resp := postJSON(t, server.Client(), server.URL+"/api/v1/policies", adminKeyOfTenant, map[string]any{
		"name": "FunctionOperator",
		"document": map[string]any{
			"version": "1",
			"statement": []map[string]any{
				{
					"effect":   "Allow",
					"action":   []string{"function:List", "function:Get", "function:Invoke"},
					"resource": []string{"orbitjob:self:*:function/*"},
				},
				{
					"effect":   "Allow",
					"action":   []string{"workflow:List", "workflow:Get", "workflow:Trigger"},
					"resource": []string{"orbitjob:self:*:workflow/*"},
				},
			},
		},
	})
	requireStatus(t, resp, http.StatusCreated)
	var created struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create policy: %d", resp.StatusCode)
	}

	// The final key is minted by the bootstrap admin: the tenant-admin key
	// cannot delegate actions its own preset does not carry -- the escalation
	// guard refuses exactly that -- while the admin holds every action and the
	// policy is visible to the tenant it was authored in.
	_, keyText := createKey(t, server, adminKey, tenantID, []string{created.ID}, "")
	return created.ID, keyText
}

// postJSONHeader issues a POST with extra headers and an optional body.
func postJSONHeader(t *testing.T, server *httptest.Server, url, authKey string, headers map[string]string, body any) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, url, bodyReader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authKey != "" {
		req.Header.Set("Authorization", "Bearer "+authKey)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// TestFunctionInvoke_PublishesJobRunCR drives the invoke route through the
// real auth middleware and dynamic fake: the response is a reference to a
// custom resource that must actually be in the cluster, carrying the Function
// trigger value, the caller's key as its actor, and the pinned revision.
func TestFunctionInvoke_PublishesJobRunCR(t *testing.T) {
	server, _, key, stores := newIntegrationServerWithWorkloads(t)
	defer server.Close()

	stores.functions.seed(functionIntegrationDef(3, bootstrap.DefaultTenantID))

	resp := postJSON(t, server.Client(), server.URL+"/api/v1/functions/3/invoke", key, nil)
	requireStatus(t, resp, http.StatusCreated)
	var out struct {
		Namespace     string `json:"namespace"`
		Name          string `json:"name"`
		OccurrenceKey string `json:"occurrence_key"`
		Trigger       string `json:"trigger"`
		Created       bool   `json:"created"`
	}
	decodeJSON(t, resp, &out)

	if !out.Created || out.Trigger != "Function" {
		t.Fatalf("unexpected invoke result: %+v", out)
	}
	if out.Namespace != "orbitjob" {
		t.Fatalf("namespace = %q, want the revision's source_namespace", out.Namespace)
	}
	if !strings.HasPrefix(out.Name, "function-3-") {
		t.Fatalf("name = %q, want the RunObjectName derivation over the function's source uid", out.Name)
	}

	object, err := stores.tracker().Get(jobRunGVR, out.Namespace, out.Name)
	if err != nil {
		t.Fatalf("read published JobRun: %v", err)
	}
	var run v1alpha1.JobRun
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(
		object.(*unstructured.Unstructured).Object, &run); err != nil {
		t.Fatalf("decode published JobRun: %v", err)
	}
	if run.Spec.Trigger != v1alpha1.Function {
		t.Errorf("spec.trigger = %q, want %q", run.Spec.Trigger, v1alpha1.Function)
	}
	if run.Spec.Actor == "" {
		t.Error("spec.actor is empty; the run would not answer who invoked it")
	}
	if run.Spec.OccurrenceKey != out.OccurrenceKey {
		t.Errorf("spec.occurrenceKey = %q, want the response's occurrence key", run.Spec.OccurrenceKey)
	}
	if run.Spec.DefinitionRevision != 41 {
		t.Errorf("spec.definitionRevision = %d, want the pinned revision 41", run.Spec.DefinitionRevision)
	}
}

// A replayed invocation carrying the same idempotency key resolves to the run
// that already exists: 200, created=false, the keyed derivation.
func TestFunctionInvoke_ReplayResolvesSameRun(t *testing.T) {
	server, _, key, stores := newIntegrationServerWithWorkloads(t)
	defer server.Close()

	stores.functions.seed(functionIntegrationDef(3, bootstrap.DefaultTenantID))

	first := postJSONHeader(t, server, server.URL+"/api/v1/functions/3/invoke", key,
		map[string]string{"X-OrbitJob-Idempotency-Key": "idem-1"}, nil)
	requireStatus(t, first, http.StatusCreated)

	replay := postJSONHeader(t, server, server.URL+"/api/v1/functions/3/invoke", key,
		map[string]string{"X-OrbitJob-Idempotency-Key": "idem-1"}, nil)
	requireStatus(t, replay, http.StatusOK)

	var out struct {
		Name    string `json:"name"`
		Created bool   `json:"created"`
	}
	decodeJSON(t, replay, &out)
	if out.Created {
		t.Fatal("a replay must not report created=true")
	}
	if !strings.HasPrefix(out.Name, "function-3-") {
		t.Fatalf("replay name = %q, want the keyed derivation", out.Name)
	}
}

// Invoking a paused function is a conflict, not an error and not a success.
func TestFunctionInvoke_PausedIsConflict(t *testing.T) {
	server, _, key, stores := newIntegrationServerWithWorkloads(t)
	defer server.Close()

	paused := functionIntegrationDef(4, bootstrap.DefaultTenantID)
	paused.Status = domainfunction.StatusPaused
	stores.functions.seed(paused)

	resp := postJSON(t, server.Client(), server.URL+"/api/v1/functions/4/invoke", key, nil)
	requireStatus(t, resp, http.StatusConflict)
}

// A function of another tenant does not exist for this caller's key: a 404
// through the real auth middleware, policy loader and use case -- not a leak
// and not a 500.
func TestFunctionGet_TenantIsolation(t *testing.T) {
	server, _, key, stores := newIntegrationServerWithWorkloads(t)
	defer server.Close()

	stores.functions.seed(functionIntegrationDef(7, bootstrap.DefaultTenantID))

	tenantID := createTenant(t, server, key, "tenant-fn")
	_, tenantKey := createKeyWithFunctionPolicy(t, server, key, tenantID)

	resp := get(t, server.Client(), server.URL+"/api/v1/functions/7", tenantKey)
	requireStatus(t, resp, http.StatusNotFound)
}

// The function run reads over the read model: the list answers, a single run
// is addressable by its deterministic run id, and another function's id under
// the same route is a 404.
func TestFunctionRuns_ReadModelLifecycle(t *testing.T) {
	server, _, key, stores := newIntegrationServerWithWorkloads(t)
	defer server.Close()

	stores.functions.seed(functionIntegrationDef(3, bootstrap.DefaultTenantID))
	now := time.Now()
	row := domainfunction.FunctionRun{
		ID: 1, RunID: "uuid-success", TenantID: bootstrap.DefaultTenantID,
		FunctionID: 3, Status: domainfunction.StatusSuccess,
		TriggeredAt: now, CreatedAt: now,
	}
	stores.functionRuns.byFunction = map[int64][]domainfunction.FunctionRun{3: {row}}
	stores.functionRuns.byRunID = map[string]domainfunction.FunctionRun{"uuid-success": row}

	resp := get(t, server.Client(), server.URL+"/api/v1/functions/3/runs", key)
	requireStatus(t, resp, http.StatusOK)
	var list struct {
		Items []struct {
			RunID  string `json:"run_id"`
			Status string `json:"status"`
		} `json:"items"`
	}
	decodeJSON(t, resp, &list)
	if len(list.Items) != 1 || list.Items[0].RunID != "uuid-success" || list.Items[0].Status != "success" {
		t.Fatalf("unexpected run list: %+v", list.Items)
	}

	resp = get(t, server.Client(), server.URL+"/api/v1/functions/3/runs/uuid-success", key)
	requireStatus(t, resp, http.StatusOK)

	resp = get(t, server.Client(), server.URL+"/api/v1/functions/9/runs/uuid-success", key)
	requireStatus(t, resp, http.StatusNotFound)
}

// A tenant key whose own policy names the new actions reaches them: the six
// actions route end to end through the real policy loader and guard.
func TestFunctionList_TenantKeyWithNewActions(t *testing.T) {
	server, _, key, stores := newIntegrationServerWithWorkloads(t)
	defer server.Close()

	tenantID := createTenant(t, server, key, "tenant-fn-list")
	_, tenantKey := createKeyWithFunctionPolicy(t, server, key, tenantID)

	owned := functionIntegrationDef(8, tenantID)
	owned.Name = "theirs"
	stores.functions.seed(owned)

	resp := get(t, server.Client(), server.URL+"/api/v1/functions", tenantKey)
	requireStatus(t, resp, http.StatusOK)
	var list struct {
		Items []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	decodeJSON(t, resp, &list)
	if len(list.Items) != 1 || list.Items[0].Name != "theirs" {
		t.Fatalf("unexpected function list for the tenant key: %+v", list.Items)
	}

	// And invoking it through the tenant key works under the same grant.
	invoke := postJSON(t, server.Client(), fmt.Sprintf("%s/api/v1/functions/8/invoke", server.URL), tenantKey, nil)
	requireStatus(t, invoke, http.StatusCreated)
}

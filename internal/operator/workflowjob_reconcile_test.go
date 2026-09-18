package operator

import (
	"context"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

// A WorkflowJob reconcile is the ScheduledJob reconcile one resource kind up:
// project the CR as an immutable revision, report the outcome on the status,
// create nothing. The tests pin the identity the workflow ledger deduplicates
// against and the status the declarer reads.

func workflowJobObject(t *testing.T) unstructured.Unstructured {
	t.Helper()
	obj := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "workloads.orbitjob.io/v1alpha1",
		"kind":       "WorkflowJob",
		"metadata": map[string]any{
			"name": "nightly-pipeline", "namespace": "finance", "uid": "wf-uid-1",
			"generation": int64(4),
		},
		"spec": map[string]any{
			"schedule":   "0 3 * * *",
			"failPolicy": "Continue",
			"tasks": []any{
				map[string]any{"name": "extract", "jobRef": map[string]any{"name": "extract-job"}},
				map[string]any{"name": "load", "jobRef": map[string]any{"name": "load-job"}, "dependsOn": []any{"extract"}},
			},
		},
	}}
	obj.SetGroupVersionKind(workflowJobGVR.GroupVersion().WithKind("WorkflowJob"))
	return obj
}

func TestReconcileWorkflowJobProjectsRevisionAndStatus(t *testing.T) {
	obj := workflowJobObject(t)
	dyn := fakeDynamic(t, &obj)
	revisions := &fakeRevisions{}
	rt := newRuntime(t, dyn, revisions, &fakeRuns{}, &fakeJobs{})

	if err := rt.ReconcileWorkflowJob(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(revisions.applied) != 1 {
		t.Fatalf("applied %d revisions, want 1", len(revisions.applied))
	}
	rev := revisions.applied[0]
	// Pinned as a literal: the mode is a storage contract shared with the
	// walker and the retainer, not a symbol to rename.
	if rev.Identity.SourceMode != "workflow" {
		t.Errorf("source_mode = %q, want workflow", rev.Identity.SourceMode)
	}
	if rev.Identity.SourceUID != "wf-uid-1" || rev.Identity.Namespace != "finance" || rev.Identity.Name != "nightly-pipeline" {
		t.Errorf("identity = %+v", rev.Identity)
	}
	if rev.Generation != 4 || rev.Actor != "orbitjob-operator" {
		t.Errorf("generation = %d actor = %q", rev.Generation, rev.Actor)
	}
	// The walker decodes the pinned revision into the task DAG it walks, so
	// the stored bytes must be the declared spec.
	var declared v1alpha1.WorkflowJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &declared); err != nil {
		t.Fatalf("walker decode of stored spec: %v", err)
	}
	if declared.Schedule != "0 3 * * *" || len(declared.Tasks) != 2 {
		t.Errorf("decoded spec = %+v", declared)
	}

	stored, err := dyn.Resource(workflowJobGVR).Namespace("finance").Get(context.Background(), "nightly-pipeline", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	status, _, _ := unstructured.NestedMap(stored.Object, "status")
	if status["observedGeneration"] != int64(4) {
		t.Errorf("observedGeneration = %v", status["observedGeneration"])
	}
	if status["activeRevision"] != int64(1) {
		t.Errorf("activeRevision = %v", status["activeRevision"])
	}
}

func TestReconcileWorkflowJobSurfacesProjectionErrors(t *testing.T) {
	obj := workflowJobObject(t)
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{applyErr: context.Canceled}, &fakeRuns{}, &fakeJobs{})
	if err := rt.ReconcileWorkflowJob(context.Background(), obj); err == nil {
		t.Fatal("expected projection error to surface")
	}

	unmapped := workflowJobObject(t)
	unmapped.SetNamespace("unknown")
	rt2 := newRuntime(t, fakeDynamic(t, &unmapped), &fakeRevisions{}, &fakeRuns{}, &fakeJobs{})
	if err := rt2.ReconcileWorkflowJob(context.Background(), unmapped); err == nil {
		t.Fatal("expected unmapped namespace to be rejected")
	}
}

func TestReconcileWorkflowJobCreatesNoRuns(t *testing.T) {
	obj := workflowJobObject(t)
	runs := &fakeRuns{}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, &fakeJobs{})
	if err := rt.ReconcileWorkflowJob(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(runs.phases) != 0 || len(runs.attempts) != 0 {
		t.Fatal("a definition projection must not touch run state")
	}
}

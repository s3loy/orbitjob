package operator

import (
	"context"
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

type prunedRun struct {
	tenant string
	id     int64
}

type fakeHistoryPruner struct {
	prunable   []workflow.Run
	prunedOf   []string
	deleted    []prunedRun
	deleteFail bool
}

func (f *fakeHistoryPruner) PrunableRuns(_ context.Context, _, sourceUID string, _, _ int) ([]workflow.Run, error) {
	f.prunedOf = append(f.prunedOf, sourceUID)
	return f.prunable, nil
}

func (f *fakeHistoryPruner) DeleteRun(_ context.Context, tenant string, runID int64) (bool, error) {
	if f.deleteFail {
		return false, errors.New("delete refused")
	}
	f.deleted = append(f.deleted, prunedRun{tenant: tenant, id: runID})
	return true, nil
}

// TestWorkflowRetainerSweepsWorkflowDefinitionsOnly pins the sweep's scope and
// order: the workflow revision is reachable only through the workflow-only
// listing (the store's ActiveRevisions never returns one, so a sweep reading
// the scheduler's list would prune nothing), the step CRs are removed before
// the atomic row delete, and an ordinary ScheduledJob's history is never
// touched by the workflow retainer.
func TestWorkflowRetainerSweepsWorkflowDefinitionsOnly(t *testing.T) {
	jobSpec := jobSpecJSON(t)
	workflowDef := withIdentity(t, workflowRevision(t, 5, workflowSpecJSON(t, twoTaskSpec())), "workflow", "wf-uid-1", "finance", "nightly-pipeline")
	jobDef := withIdentity(t, workflowRevision(t, 10, jobSpec), "kubernetes", "extract-job-uid", "finance", "extract-job")
	defs := &fakeWorkflowRevisions{
		active:         []revision.Revision{jobDef},
		workflowActive: []revision.Revision{workflowDef},
	}

	run := workflow.Run{ID: 42, ResourceUID: "parent-uid", RevisionID: 5, Trigger: jobrun.Manual, Actor: "operator", SourceUID: "wf-uid-1", OccurrenceKey: "wocc-1", Phase: workflow.PhaseSucceeded}
	pruner := &fakeHistoryPruner{prunable: []workflow.Run{run}}
	steps := []workflow.StepRun{stepFor(t, "extract", jobrun.Succeeded, 1)}
	steps[0].RevisionID = 10
	runs := &fakeWorkflowRuns{stored: run, steps: steps}

	// The step's CR exists in the definition's namespace, named by the shared
	// derivation the publisher used.
	stepCR := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "workloads.orbitjob.io/v1alpha1",
		"kind":       "JobRun",
		"metadata": map[string]any{
			"name":      v1alpha1.RunObjectName("extract-job", steps[0].OccurrenceKey),
			"namespace": "finance", "uid": "step-uid", "resourceVersion": "1",
		},
		"spec":   map[string]any{"scheduledJobRef": map[string]any{"name": "extract-job", "uid": "extract-job-uid"}, "definitionRevision": int64(10), "trigger": "Workflow", "actor": ActorWorkflowWalker, "occurrenceKey": steps[0].OccurrenceKey},
		"status": map[string]any{"phase": "Succeeded"},
	}}
	parentCR := unstructured.Unstructured{Object: map[string]any{"apiVersion": v1alpha1.GroupVersion.String(), "kind": "WorkflowRun", "metadata": map[string]any{"name": v1alpha1.WorkflowRunObjectName("nightly-pipeline", run.OccurrenceKey), "namespace": "finance", "uid": "parent-uid", "resourceVersion": "1"}, "spec": map[string]any{"workflowRef": map[string]any{"uid": run.SourceUID, "name": "nightly-pipeline"}, "definitionRevision": run.RevisionID, "occurrenceKey": run.OccurrenceKey, "trigger": string(run.Trigger), "actor": "operator"}}}
	if err := unstructured.SetNestedField(parentCR.Object, "Succeeded", "status", "phase"); err != nil {
		t.Fatal(err)
	}
	dyn := fakeDynamic(t, &stepCR, &parentCR)
	retainer := WorkflowRetainer{
		Definitions: defs,
		Workflows:   runs,
		History:     pruner,
		Remover:     RunRemover{Client: dyn},
		Tenants:     []string{executionTenant},
	}

	removed, err := retainer.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if len(pruner.prunedOf) != 1 || pruner.prunedOf[0] != "wf-uid-1" {
		t.Fatalf("pruner queried for %+v, want only the workflow definition", pruner.prunedOf)
	}
	if len(pruner.deleted) != 1 || pruner.deleted[0] != (prunedRun{tenant: executionTenant, id: 42}) {
		t.Fatalf("deleted rows = %+v, want the workflow run", pruner.deleted)
	}
	_, err = dyn.Resource(jobRunGVR).Namespace("finance").Get(context.Background(),
		v1alpha1.RunObjectName("extract-job", steps[0].OccurrenceKey), metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("step CR still present after sweep: %v", err)
	}
	if _, err := dyn.Resource(workflowRunGVR).Namespace("finance").Get(context.Background(), parentCR.GetName(), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("parent trigger can rematerialize pruned workflow: %v", err)
	}
	// An already queued stale informer object must not resurrect the pruned row.
	rt := Runtime{Dynamic: dyn, Revisions: &fakeRevisions{}, Workflows: runs, Tenants: NamespaceTenantResolver{Tenants: map[string]string{"finance": executionTenant}}}
	runs.found = false
	if err := rt.ReconcileWorkflowRun(context.Background(), parentCR); err != nil {
		t.Fatal(err)
	}
	if len(runs.created) != 0 {
		t.Fatal("deleted workflow trigger was rematerialized")
	}

}

// TestWorkflowRetainerSurvivesGoneDefinitions pins the skip: a step whose
// definition was deleted has no live CR (the owner reference collected it), so
// the sweep skips it instead of failing the whole sweep.
func TestWorkflowRetainerSurvivesGoneDefinitions(t *testing.T) {
	workflowDef := withIdentity(t, workflowRevision(t, 5, workflowSpecJSON(t, twoTaskSpec())), "workflow", "wf-uid-1", "finance", "nightly-pipeline")
	defs := &fakeWorkflowRevisions{workflowActive: []revision.Revision{workflowDef}}
	run := workflow.Run{ID: 42, ResourceUID: "resource-uid", SourceUID: "wf-uid-1", OccurrenceKey: "wocc-1", Phase: workflow.PhaseFailed}
	pruner := &fakeHistoryPruner{prunable: []workflow.Run{run}}
	// The step's source uid resolves to no active definition.
	steps := []workflow.StepRun{{
		ID: 1, WorkflowRunID: 42, SourceUID: "deleted-definition-uid",
		OccurrenceKey: workflow.StepOccurrenceKey("wf-uid-1", "wocc-1", "extract"),
		Trigger:       "Workflow", Phase: "Failed", Attempt: 3,
		CreatedAt: time.Unix(1699999000, 0).UTC(),
	}}
	runs := &fakeWorkflowRuns{stored: run, steps: steps}

	retainer := WorkflowRetainer{
		Definitions: defs,
		Workflows:   runs,
		History:     pruner,
		Remover:     RunRemover{Client: fakeDynamic(t)},
		Tenants:     []string{executionTenant},
	}
	removed, err := retainer.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 || len(pruner.deleted) != 1 {
		t.Fatalf("removed=%d deleted=%+v, want the row pruned despite the gone definition", removed, pruner.deleted)
	}
}

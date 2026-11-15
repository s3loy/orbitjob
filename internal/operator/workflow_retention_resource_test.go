package operator

import (
	"context"
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

func retainedWorkflowFixture() (revision.Identity, workflow.Run, v1alpha1.WorkflowRun) {
	definition := revision.Identity{SourceMode: "workflow", SourceUID: "workflow-uid", Namespace: "finance", Name: "pipeline"}
	row := workflow.Run{ID: 8, SourceUID: definition.SourceUID, ResourceUID: "original-run-uid", RevisionID: 5, OccurrenceKey: "occurrence-key", Trigger: "Manual", Actor: "key-1", Phase: workflow.PhaseRejected, Reason: "concurrency_forbid"}
	obj := v1alpha1.WorkflowRun{
		TypeMeta:   metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "WorkflowRun"},
		ObjectMeta: metav1.ObjectMeta{Name: v1alpha1.WorkflowRunObjectName(definition.Name, row.OccurrenceKey), Namespace: definition.Namespace, UID: "original-run-uid", ResourceVersion: "7"},
		Spec:       v1alpha1.WorkflowRunSpec{WorkflowRef: v1alpha1.ObjectReference{Name: definition.Name, UID: definition.SourceUID}, DefinitionRevision: 5, OccurrenceKey: row.OccurrenceKey, Trigger: v1alpha1.Manual, Actor: row.Actor},
		Status:     v1alpha1.WorkflowRunStatus{Phase: "Rejected", Reason: "concurrency_forbid"},
	}
	return definition, row, obj
}

func TestWorkflowRetentionDeletesOnlyOriginalTerminalInstance(t *testing.T) {
	definition, row, obj := retainedWorkflowFixture()
	client := fakeDynamic(t, &obj)
	fake := client.(*dynamicfake.FakeDynamicClient)
	checked := false
	fake.PrependReactor("delete", "workflowruns", func(action clienttesting.Action) (bool, runtime.Object, error) {
		options := action.(clienttesting.DeleteAction).GetDeleteOptions()
		if options.Preconditions == nil || options.Preconditions.UID == nil || *options.Preconditions.UID != obj.UID || options.Preconditions.ResourceVersion == nil || *options.Preconditions.ResourceVersion != "7" {
			t.Fatalf("missing instance preconditions: %+v", options)
		}
		checked = true
		return false, nil, nil
	})
	remover := RunRemover{Client: client}
	if err := remover.RemoveWorkflow(context.Background(), definition, row); err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("no guarded delete")
	}
	if err := remover.RemoveWorkflow(context.Background(), definition, row); err != nil {
		t.Fatalf("NotFound must be idempotent: %v", err)
	}
}

func TestWorkflowRetentionRefusesForeignReplacementAndActiveResources(t *testing.T) {
	for _, name := range []string{"source", "revision", "occurrence", "actor", "trigger", "replacement", "active", "terminal_mismatch", "no_binding"} {
		t.Run(name, func(t *testing.T) {
			definition, row, obj := retainedWorkflowFixture()
			switch name {
			case "source":
				obj.Spec.WorkflowRef.UID = "foreign"
			case "revision":
				obj.Spec.DefinitionRevision = 6
			case "occurrence":
				obj.Spec.OccurrenceKey = "other"
			case "actor":
				obj.Spec.Actor = "other-key"
			case "trigger":
				obj.Spec.Trigger = v1alpha1.Schedule
			case "replacement":
				obj.UID = "replacement-uid"
			case "active":
				obj.Status.Phase = "Running"
			case "terminal_mismatch":
				obj.Status.Phase = "Failed"
			case "no_binding":
				row.ResourceUID = ""
			}
			client := fakeDynamic(t, &obj)
			if err := (RunRemover{Client: client}).RemoveWorkflow(context.Background(), definition, row); err == nil {
				t.Fatal("unsafe resource was deleted")
			}
			for _, action := range client.(*dynamicfake.FakeDynamicClient).Actions() {
				if action.GetVerb() == "delete" {
					t.Fatal("unsafe resource reached delete")
				}
			}
		})
	}
}

func TestWorkflowRetentionReplacementDuringDeletePreservesRow(t *testing.T) {
	definition, row, obj := retainedWorkflowFixture()
	client := fakeDynamic(t, &obj)
	client.(*dynamicfake.FakeDynamicClient).PrependReactor("delete", "workflowruns", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "workflowruns"}, obj.Name, errors.New("UID changed"))
	})
	pruner := &fakeHistoryPruner{prunable: []workflow.Run{row}}
	rev := workflowRevision(t, 5, workflowSpecJSON(t, twoTaskSpec()))
	rev.Identity = definition
	retainer := WorkflowRetainer{Definitions: &fakeWorkflowRevisions{workflowActive: []revision.Revision{rev}}, Workflows: &fakeWorkflowRuns{stored: row, found: true}, History: pruner, Remover: RunRemover{Client: client}, Tenants: []string{"tenant-a"}}
	if _, err := retainer.Sweep(context.Background()); err == nil {
		t.Fatal("delete conflict was ignored")
	}
	if len(pruner.deleted) != 0 {
		t.Fatal("row was pruned after replacement conflict")
	}
}

func TestManualWorkflowStaleWatchCannotRecreatePrunedOccurrence(t *testing.T) {
	obj := workflowRunObject(t, nil)
	runs := &fakeWorkflowRuns{}
	runtime := newWorkflowRuntime(t, &fakeWorkflowRevisions{}, runs)
	if err := runtime.ReconcileWorkflowRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(runs.created) != 0 {
		t.Fatal("stale informer resource recreated pruned row")
	}
}

func TestWorkflowRetentionKeepsUnboundHistoricalRows(t *testing.T) {
	definition, row, _ := retainedWorkflowFixture()
	row.ResourceUID = ""
	pruner := &fakeHistoryPruner{prunable: []workflow.Run{row}}
	rev := workflowRevision(t, 5, workflowSpecJSON(t, twoTaskSpec()))
	rev.Identity = definition
	retainer := WorkflowRetainer{Definitions: &fakeWorkflowRevisions{workflowActive: []revision.Revision{rev}}, Workflows: &fakeWorkflowRuns{}, History: pruner, Remover: RunRemover{Client: fakeDynamic(t)}, Tenants: []string{"tenant-a"}}
	if count, err := retainer.Sweep(context.Background()); err != nil || count != 0 || len(pruner.deleted) != 0 {
		t.Fatalf("unbound historical row was pruned: count=%d err=%v", count, err)
	}
}

func TestWorkflowRetentionKeepsSkippedOccurrenceThroughFiringMinute(t *testing.T) {
	definition, row, _ := retainedWorkflowFixture()
	now := time.Date(2026, 10, 24, 12, 0, 30, 0, time.UTC)
	planned := now.Truncate(time.Minute)
	row.Trigger = "Schedule"
	row.ResourceUID = ""
	row.ScheduledFor = &planned
	pruner := &fakeHistoryPruner{prunable: []workflow.Run{row}}
	rev := workflowRevision(t, 5, workflowSpecJSON(t, twoTaskSpec()))
	rev.Identity = definition
	retainer := WorkflowRetainer{Definitions: &fakeWorkflowRevisions{workflowActive: []revision.Revision{rev}}, Workflows: &fakeWorkflowRuns{}, History: pruner, Remover: RunRemover{Client: fakeDynamic(t)}, Tenants: []string{"tenant-a"}, Now: func() time.Time { return now }}
	if count, err := retainer.Sweep(context.Background()); err != nil || count != 0 || len(pruner.deleted) != 0 {
		t.Fatalf("active skip receipt was pruned: count=%d err=%v", count, err)
	}
	retainer.Now = func() time.Time { return now.Add(time.Minute) }
	if count, err := retainer.Sweep(context.Background()); err != nil || count != 1 || len(pruner.deleted) != 1 {
		t.Fatalf("expired skip receipt was not prunable: count=%d err=%v", count, err)
	}
}

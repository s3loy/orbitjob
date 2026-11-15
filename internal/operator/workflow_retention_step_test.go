package operator

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

func retainedStepFixture() (revision.Identity, workflow.Run, workflow.StepRun, v1alpha1.JobRun) {
	definition := revision.Identity{SourceMode: "kubernetes", SourceUID: "job-uid", Namespace: "tasks", Name: "extract"}
	parent := workflow.Run{ID: 10, Phase: workflow.PhaseSucceeded}
	row := workflow.StepRun{ID: 20, WorkflowRunID: 10, SourceUID: definition.SourceUID, RevisionID: 5, OccurrenceKey: "12345678full-step-occurrence", Trigger: jobrun.Workflow, Phase: jobrun.Succeeded}
	obj := v1alpha1.JobRun{TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "JobRun"}, ObjectMeta: metav1.ObjectMeta{Name: v1alpha1.RunObjectName(definition.Name, row.OccurrenceKey), Namespace: definition.Namespace, UID: "original-step-uid", ResourceVersion: "8"}, Spec: v1alpha1.JobRunSpec{ScheduledJobRef: v1alpha1.ObjectReference{Name: definition.Name, UID: definition.SourceUID}, DefinitionRevision: row.RevisionID, OccurrenceKey: row.OccurrenceKey, Trigger: v1alpha1.Workflow, Actor: ActorWorkflowWalker}, Status: v1alpha1.JobRunStatus{Phase: "Succeeded"}}
	return definition, parent, row, obj
}

func TestWorkflowStepRetentionUsesFullIdentityAndInstancePreconditions(t *testing.T) {
	definition, parent, row, obj := retainedStepFixture()
	client := fakeDynamic(t, &obj)
	checked := false
	client.(*dynamicfake.FakeDynamicClient).PrependReactor("delete", "jobruns", func(action clienttesting.Action) (bool, runtime.Object, error) {
		options := action.(clienttesting.DeleteAction).GetDeleteOptions()
		if options.Preconditions == nil || options.Preconditions.UID == nil || *options.Preconditions.UID != obj.UID || options.Preconditions.ResourceVersion == nil || *options.Preconditions.ResourceVersion != "8" {
			t.Fatalf("missing instance preconditions: %+v", options)
		}
		checked = true
		return false, nil, nil
	})
	remover := RunRemover{Client: client}
	if err := remover.RemoveWorkflowStep(context.Background(), definition, parent, row); err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("step delete was not guarded")
	}
	if err := remover.RemoveWorkflowStep(context.Background(), definition, parent, row); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowStepRetentionRefusesCollisionsForeignAndActiveResources(t *testing.T) {
	for _, field := range []string{"source", "name", "revision", "full_occurrence", "trigger", "active_cr", "active_ledger", "parent", "no_uid", "no_version"} {
		t.Run(field, func(t *testing.T) {
			definition, parent, row, obj := retainedStepFixture()
			switch field {
			case "source":
				obj.Spec.ScheduledJobRef.UID = "foreign"
			case "name":
				obj.Spec.ScheduledJobRef.Name = "foreign"
			case "revision":
				obj.Spec.DefinitionRevision = 9
			case "full_occurrence":
				obj.Spec.OccurrenceKey = "12345678foreign-collision"
			case "trigger":
				obj.Spec.Trigger = v1alpha1.Manual
			case "active_cr":
				obj.Status.Phase = "Running"
			case "active_ledger":
				row.Phase = jobrun.Running
			case "parent":
				row.WorkflowRunID = 99
			case "no_uid":
				obj.UID = ""
			case "no_version":
				obj.ResourceVersion = ""
			}
			client := fakeDynamic(t, &obj)
			if err := (RunRemover{Client: client}).RemoveWorkflowStep(context.Background(), definition, parent, row); err == nil {
				t.Fatal("unsafe step was deleted")
			}
			for _, action := range client.(*dynamicfake.FakeDynamicClient).Actions() {
				if action.GetVerb() == "delete" {
					t.Fatal("unsafe step reached delete")
				}
			}
		})
	}
}

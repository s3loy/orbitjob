package operator

import (
	"context"
	"testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

func TestWorkflowMissingReferencePersistsFailureAndReplays(t *testing.T) {
	for _, mode := range []string{"absent", "foreign-namespace", "function", "workflow"} {
		t.Run(mode, func(t *testing.T) {
			runs := &fakeWorkflowRuns{stored: walkerRun()}
			revs := walkerRevisions(t)
			revs.active = nil
			if mode != "absent" {
				ns, kind := "finance", mode
				if mode == "foreign-namespace" {
					ns = "another"
					kind = "kubernetes"
				}
				revs.active = []revision.Revision{withIdentity(t, revs.byID[10], kind, "foreign-uid", ns, "extract-job")}
			}
			pub := &fakeWorkflowPublisher{}
			walker, projected := newWalker(t, runs, revs, pub)
			for i := 0; i < 2; i++ {
				if i == 1 {
					runs.openRuns = []workflow.Run{}
				}
				if n, err := walker.Tick(context.Background()); err != nil || n != 0 {
					t.Fatalf("tick: %d %v", n, err)
				}
			}
			if runs.stored.Phase != workflow.PhaseFailed || len(*projected) != 1 || (*projected)[0].TaskDecisions["extract"].Reason != workflow.ReasonMissingJob {
				t.Fatalf("missing ref did not persist clear failure: %+v", runs.stored)
			}
			if len(pub.published) != 0 {
				t.Fatal("published incompatible definition")
			}
		})
	}
}

func TestWorkflowDeadlineUnfinishedFailsButLateCompletedSucceeds(t *testing.T) {
	for _, completed := range []bool{false, true} {
		runs := &fakeWorkflowRuns{stored: walkerRun()}
		revs := walkerRevisions(t)
		spec := twoTaskSpec()
		spec.TimeoutSeconds = 1
		revs.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, spec))
		if completed {
			runs.steps = []workflow.StepRun{stepFor(t, "extract", jobrun.Succeeded, 1), stepFor(t, "load", jobrun.Succeeded, 1)}
		}
		walker, _ := newWalker(t, runs, revs, &fakeWorkflowPublisher{})
		if _, err := walker.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		want := workflow.PhaseFailed
		if completed {
			want = workflow.PhaseSucceeded
		}
		if runs.stored.Phase != want {
			t.Fatalf("completed=%v phase=%s want=%s decisions=%+v", completed, runs.stored.Phase, want, runs.stored.TaskDecisions)
		}
	}
}

func TestLateObservationPreservesLegitimateConditionSkip(t *testing.T) {
	spec := twoTaskSpec()
	spec.TimeoutSeconds = 1
	spec.Tasks[1].Condition = &v1alpha1.WorkflowConditionSpec{Type: v1alpha1.ConditionAll, Rules: []v1alpha1.WorkflowRule{{Task: "extract", Field: v1alpha1.RuleFieldPhase, Operator: v1alpha1.OperatorEq, Values: []string{"Failed"}}}}
	revs := walkerRevisions(t)
	revs.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, spec))
	runs := &fakeWorkflowRuns{stored: walkerRun(), steps: []workflow.StepRun{stepFor(t, "extract", jobrun.Succeeded, 1)}}
	walker, projected := newWalker(t, runs, revs, &fakeWorkflowPublisher{})
	if _, err := walker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runs.stored.Phase != workflow.PhaseSucceeded || len(*projected) != 1 || (*projected)[0].TaskDecisions["load"].Reason != workflow.ReasonCondition {
		t.Fatalf("condition skip reclassified by observation time: %+v", *projected)
	}
}

func TestMissingReferenceFailureKeepsFailFastOnLaterTick(t *testing.T) {
	spec := twoTaskSpec()
	spec.FailPolicy = v1alpha1.FailFast
	spec.Tasks = append(spec.Tasks, v1alpha1.WorkflowTaskSpec{Name: "missing", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "gone"}})
	revs := walkerRevisions(t)
	revs.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, spec))
	run := walkerRun()
	run.TaskDecisions = map[string]workflow.TaskDecision{"missing": {Skipped: true, Reason: workflow.ReasonMissingJob}}
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{stepFor(t, "extract", jobrun.Succeeded, 1)}}
	walker, projected := newWalker(t, runs, revs, &fakeWorkflowPublisher{})
	if _, err := walker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runs.createdStep) != 0 || runs.stored.Phase != workflow.PhaseFailed || len(*projected) != 1 || (*projected)[0].TaskDecisions["load"].Reason != workflow.ReasonFailFast {
		t.Fatalf("forgot earlier failure: steps=%+v result=%+v", runs.createdStep, *projected)
	}
}

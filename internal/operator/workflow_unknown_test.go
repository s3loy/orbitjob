package operator

import (
	"testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
)

func TestUnknownWorkflowObservesSiblingsAndBlocksNewIndependentTasks(t *testing.T) {
	run := walkerRun()
	revs := walkerRevisions(t)
	spec := twoTaskSpec()
	spec.Tasks[1].DependsOn = nil
	spec.Tasks = append(spec.Tasks, v1alpha1.WorkflowTaskSpec{Name: "later", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "load-job"}})
	revs.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, spec))
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{stepFor(t, "extract", jobrun.ExecutionUnknown, 1), stepFor(t, "load", jobrun.Running, 1)}}
	pub := &fakeWorkflowPublisher{}
	w, _ := newWalker(t, runs, revs, pub)
	if n, err := w.Tick(t.Context()); err != nil || n != 0 {
		t.Fatalf("unknown workflow advanced: %d %v", n, err)
	}
	if runs.stored.Phase != workflow.PhaseExecutionUnknown || len(runs.createdStep) != 0 || len(pub.published) != 2 {
		t.Fatalf("unknown did not isolate while observing siblings: phase=%s new=%d observations=%d", runs.stored.Phase, len(runs.createdStep), len(pub.published))
	}
	runs.steps[0].Phase = jobrun.Succeeded
	if n, err := w.Tick(t.Context()); err != nil || n != 0 {
		t.Fatalf("recovery pass: %d %v", n, err)
	}
	if runs.stored.Phase != workflow.PhaseRunning {
		t.Fatalf("original fact did not resume parent: %s", runs.stored.Phase)
	}
	if n, err := w.Tick(t.Context()); err != nil || n != 1 {
		t.Fatalf("recovered workflow cannot resume: %d %v", n, err)
	}
}

func TestCommittedWorkflowCancelConvergesAfterSuccessfulChildren(t *testing.T) {
	run := walkerRun()
	run.Phase = workflow.PhaseCancelRequested
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{stepFor(t, "extract", jobrun.Succeeded, 1), stepFor(t, "load", jobrun.Succeeded, 1)}}
	w, _ := newWalker(t, runs, walkerRevisions(t), &fakeWorkflowPublisher{})
	if _, err := w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if runs.stored.Phase != workflow.PhaseCanceled {
		t.Fatalf("committed cancel was overwritten or stranded: %s", runs.stored.Phase)
	}
}

func TestChildCancelUnknownDoesNotCancelItsWorkflowSiblings(t *testing.T) {
	run := walkerRun()
	revs := walkerRevisions(t)
	spec := twoTaskSpec()
	spec.Tasks[1].DependsOn = nil
	spec.Tasks = append(spec.Tasks, v1alpha1.WorkflowTaskSpec{Name: "later", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "load-job"}})
	revs.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, spec))
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{stepFor(t, "extract", jobrun.CancelUnknown, 1), stepFor(t, "load", jobrun.Running, 1)}}
	pub := &fakeWorkflowPublisher{}
	w, _ := newWalker(t, runs, revs, pub)
	for range 2 {
		if n, err := w.Tick(t.Context()); err != nil || n != 0 {
			t.Fatalf("uncertain child created new work: %d %v", n, err)
		}
		if runs.stored.Phase != workflow.PhaseExecutionUnknown || len(pub.cancels) != 0 || len(runs.createdStep) != 0 {
			t.Fatalf("child stop escaped its scope: phase=%s cancels=%v new=%v", runs.stored.Phase, pub.cancels, runs.createdStep)
		}
	}
}

func TestUnknownDependencyCannotResolveCondition(t *testing.T) {
	task := workflow.Task{Name: "later", DependsOn: []string{"prior"}}
	if dependenciesResolved(task, map[string]workflow.StepState{"prior": {Phase: jobrun.ExecutionUnknown}}, nil, nil) {
		t.Fatal("unknown execution was used as a resolved dependency")
	}
}

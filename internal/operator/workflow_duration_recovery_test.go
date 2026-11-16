package operator

import (
	"context"
	"errors"
	"strings"
	"testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
)

func durationConditionSpec() v1alpha1.WorkflowJobSpec {
	spec := twoTaskSpec()
	threshold := int64(1000)
	spec.Tasks[1].Condition = &v1alpha1.WorkflowConditionSpec{Type: v1alpha1.ConditionAll, Rules: []v1alpha1.WorkflowRule{
		{Task: "extract", Field: v1alpha1.RuleFieldDurationMs, Operator: v1alpha1.OperatorLe, Number: &threshold},
	}}
	return spec
}

func TestWorkflowCancellationNeedsNoUnusedDurationOperands(t *testing.T) {
	for _, phase := range []workflow.Phase{workflow.PhaseRunning, workflow.PhaseCancelRequested, workflow.PhaseCancelUnknown} {
		t.Run(string(phase), func(t *testing.T) {
			run := walkerRun()
			run.Phase = phase
			runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{stepFor(t, "extract", jobrun.Canceled, 0)}}
			revs := walkerRevisions(t)
			revs.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, durationConditionSpec()))
			pub := &fakeWorkflowPublisher{}
			walker, projected := newWalker(t, runs, revs, pub)
			walker.ObserveResource = func(context.Context, string, workflow.Run) (bool, error) { return true, nil }
			outcomes := &fakeTerminalOutcomes{err: errors.New("timing is unavailable")}
			walker.Outcomes = outcomes
			created, err := walker.advanceRun(t.Context(), executionTenant, run, definitionIndex(revs.active))
			if err != nil || created != 0 || len(runs.createdStep) != 0 {
				t.Fatalf("cancellation created=%d steps=%+v error=%v", created, runs.createdStep, err)
			}
			if outcomes.calls != 0 {
				t.Fatalf("cancellation read %d unused timing operands", outcomes.calls)
			}
			if len(runs.phases) == 0 || runs.phases[len(runs.phases)-1].phase != workflow.PhaseCanceled {
				t.Fatalf("cancellation did not converge: %+v", runs.phases)
			}
			if len(*projected) != 1 || (*projected)[0].Phase != workflow.PhaseCanceled || (*projected)[0].TaskDecisions["load"].Reason != workflow.ReasonCanceled {
				t.Fatalf("cancellation projection = %+v", *projected)
			}
		})
	}
}

func TestWorkflowCancellationStillFansOutWithUntimedTerminalSibling(t *testing.T) {
	run := walkerRun()
	run.Phase = workflow.PhaseCancelRequested
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{
		stepFor(t, "extract", jobrun.Canceled, 0), stepFor(t, "load", jobrun.Running, 1),
	}}
	revs := walkerRevisions(t)
	revs.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, durationConditionSpec()))
	pub := &fakeWorkflowPublisher{}
	walker, _ := newWalker(t, runs, revs, pub)
	if _, err := walker.advanceRun(t.Context(), executionTenant, run, definitionIndex(revs.active)); err != nil {
		t.Fatal(err)
	}
	want := v1alpha1.RunObjectName("load-job", stepKey(t, "load"))
	if len(pub.cancels) != 1 || pub.cancels[0] != want || len(runs.phases) != 0 {
		t.Fatalf("cancel fanout=%v phases=%+v", pub.cancels, runs.phases)
	}
	runs.steps[1].Phase = jobrun.Canceled
	if _, err := walker.advanceRun(t.Context(), executionTenant, run, definitionIndex(revs.active)); err != nil {
		t.Fatal(err)
	}
	if len(runs.phases) != 1 || runs.phases[0].phase != workflow.PhaseCanceled {
		t.Fatalf("confirmed cancellation did not converge: %+v", runs.phases)
	}
}

func TestWorkflowDurationConditionRefusesUnavailableTiming(t *testing.T) {
	for _, tc := range []struct {
		name     string
		outcomes *fakeTerminalOutcomes
		message  string
	}{
		{"no reader", nil, "require outcome reader"},
		{"no attempt timing", &fakeTerminalOutcomes{}, "no measured duration"},
		{"read failure", &fakeTerminalOutcomes{err: errors.New("timing read failed")}, "timing read failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runs := &fakeWorkflowRuns{stored: walkerRun(), steps: []workflow.StepRun{stepFor(t, "extract", jobrun.Canceled, 0)}}
			revs := walkerRevisions(t)
			revs.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, durationConditionSpec()))
			pub := &fakeWorkflowPublisher{}
			walker, _ := newWalker(t, runs, revs, pub)
			if tc.outcomes != nil {
				walker.Outcomes = tc.outcomes
			}
			_, err := walker.advanceRun(t.Context(), executionTenant, runs.stored, definitionIndex(revs.active))
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("error=%v, want %q", err, tc.message)
			}
			if len(runs.createdStep) != 0 || len(runs.phases) != 0 || len(pub.cancels) != 0 {
				t.Fatalf("missing duration changed execution: steps=%+v phases=%+v cancels=%v", runs.createdStep, runs.phases, pub.cancels)
			}
		})
	}
}

func TestWorkflowCompletedConditionDoesNotReadDurationsAgain(t *testing.T) {
	runs := &fakeWorkflowRuns{stored: walkerRun(), steps: []workflow.StepRun{
		stepFor(t, "extract", jobrun.Canceled, 0), stepFor(t, "load", jobrun.Canceled, 0),
	}}
	revs := walkerRevisions(t)
	revs.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, durationConditionSpec()))
	walker, _ := newWalker(t, runs, revs, &fakeWorkflowPublisher{})
	if _, err := walker.advanceRun(t.Context(), executionTenant, runs.stored, definitionIndex(revs.active)); err != nil {
		t.Fatal(err)
	}
	if len(runs.phases) != 1 || runs.phases[0].phase != workflow.PhaseCanceled {
		t.Fatalf("phase=%+v", runs.phases)
	}
}

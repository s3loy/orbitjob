package operator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/controlplane"
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

func TestWorkflowDeadlineStopsSiblingWithDurationConditionPending(t *testing.T) {
	start := walkerRun().CreatedAt.Add(time.Second)
	for _, tc := range []struct {
		name       string
		attempt    int
		outcomes   *fakeTerminalOutcomes
		wantReason string
		wantPhase  workflow.Phase
	}{
		{"no attempt timing", 0, &fakeTerminalOutcomes{}, workflow.ReasonDeadline, workflow.PhaseFailed},
		{"no reader", 1, nil, workflow.ReasonDeadline, workflow.PhaseFailed},
		{"read failure", 1, &fakeTerminalOutcomes{err: errors.New("timing read failed")}, workflow.ReasonDeadline, workflow.PhaseFailed},
		{"measured false condition", 1, &fakeTerminalOutcomes{outcome: controlplane.TerminalOutcome{
			StartedAt: start, CompletedAt: start.Add(2 * time.Second),
		}}, workflow.ReasonCondition, workflow.PhaseCanceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := durationConditionSpec()
			spec.FailPolicy = v1alpha1.Continue
			spec.TimeoutSeconds = 60
			spec.Tasks = append(spec.Tasks, v1alpha1.WorkflowTaskSpec{Name: "independent", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "independent-job"}})
			run := walkerRun()
			run.Phase = workflow.PhaseRunning
			independent := stepFor(t, "independent", jobrun.Running, 1)
			independent.ID, independent.RevisionID = 2, 12
			runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{stepFor(t, "extract", jobrun.Canceled, tc.attempt), independent}}
			revs := walkerRevisions(t)
			revs.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, spec))
			revs.active = append(revs.active, withIdentity(t, workflowRevision(t, 12, jobSpecJSON(t)), "kubernetes", independent.SourceUID, "finance", "independent-job"))
			pub := &fakeWorkflowPublisher{}
			walker, projected := newWalker(t, runs, revs, pub)
			if tc.outcomes != nil {
				walker.Outcomes = tc.outcomes
			}

			if n, err := walker.Tick(t.Context()); err != nil || n != 0 || len(runs.createdStep) != 0 {
				t.Fatalf("expired workflow: created=%d rows=%+v error=%v", n, runs.createdStep, err)
			}
			want := v1alpha1.RunObjectName("independent-job", independent.OccurrenceKey)
			if len(pub.cancels) != 1 || pub.cancels[0] != want {
				t.Fatalf("stop requests=%v, want [%s]", pub.cancels, want)
			}
			if len(*projected) != 1 || (*projected)[0].Phase != workflow.PhaseRunning ||
				(*projected)[0].TaskDecisions["load"] != (workflow.TaskDecision{Skipped: true, Reason: tc.wantReason}) {
				t.Fatalf("expected pending load to be skipped with %q while the sibling stops: %+v", tc.wantReason, *projected)
			}

			runs.stored = (*projected)[0]
			runs.steps[1].Phase = jobrun.Canceled
			if n, err := walker.Tick(t.Context()); err != nil || n != 0 || len(pub.cancels) != 1 || runs.stored.Phase != tc.wantPhase {
				t.Fatalf("after confirmed stop: created=%d stop requests=%v phase=%s want=%s error=%v", n, pub.cancels, runs.stored.Phase, tc.wantPhase, err)
			}
		})
	}
}

type workflowDurationReader func() (controlplane.TerminalOutcome, error)

func (read workflowDurationReader) TerminalOutcome(context.Context, string, int64) (controlplane.TerminalOutcome, error) {
	return read()
}

func TestWorkflowDeadlineElapsedDuringDurationRead(t *testing.T) {
	for _, tc := range []struct {
		name       string
		duration   time.Duration
		readErr    error
		wantReason string
	}{
		{"passing condition", 500 * time.Millisecond, nil, workflow.ReasonDeadline},
		{"false condition", 2 * time.Second, nil, workflow.ReasonCondition},
		{"failed read", 0, errors.New("failed duration read"), workflow.ReasonDeadline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := durationConditionSpec()
			spec.TimeoutSeconds = 60
			spec.Tasks = append(spec.Tasks, v1alpha1.WorkflowTaskSpec{Name: "independent", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "extract-job"}})
			run := walkerRun()
			runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{stepFor(t, "extract", jobrun.Succeeded, 1)}}
			revs := walkerRevisions(t)
			revs.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, spec))
			pub := &fakeWorkflowPublisher{}
			walker, projected := newWalker(t, runs, revs, pub)
			now := run.CreatedAt.Add(59 * time.Second)
			walker.Now = func() time.Time { return now }
			walker.Outcomes = workflowDurationReader(func() (controlplane.TerminalOutcome, error) {
				now = run.CreatedAt.Add(61 * time.Second)
				start := run.CreatedAt.Add(time.Second)
				return controlplane.TerminalOutcome{StartedAt: start, CompletedAt: start.Add(tc.duration)}, tc.readErr
			})

			n, err := walker.advanceRun(t.Context(), executionTenant, run, definitionIndex(revs.active))
			if err != nil || n != 1 || len(runs.createdStep) != 1 || runs.createdStep[0].OccurrenceKey != stepKey(t, "independent") {
				t.Fatalf("only the independent task was ready before expiry: created=%d steps=%+v error=%v", n, runs.createdStep, err)
			}
			want := v1alpha1.RunObjectName("extract-job", stepKey(t, "independent"))
			if len(pub.cancels) != 1 || pub.cancels[0] != want {
				t.Fatalf("expected a stop for the step created before expiry, got %v", pub.cancels)
			}
			if len(*projected) != 1 || (*projected)[0].TaskDecisions["load"] != (workflow.TaskDecision{Skipped: true, Reason: tc.wantReason}) {
				t.Fatalf("expected load to be skipped with %q: %+v", tc.wantReason, *projected)
			}
		})
	}
}

type workflowAdmissionClock struct {
	*fakeWorkflowRuns
	advance func()
}

func (f workflowAdmissionClock) CreateStepForTenant(ctx context.Context, tenant string, runID int64, step workflow.StepRun, attempts int, actor string) (workflow.StepRun, bool, error) {
	f.advance()
	return f.fakeWorkflowRuns.CreateStepForTenant(ctx, tenant, runID, step, attempts, actor)
}

func TestWorkflowDeadlineElapsedDuringStepAdmission(t *testing.T) {
	for _, tc := range []struct {
		name       string
		admission  error
		wantCreate int
	}{
		{"committed", nil, 1},
		{"blocked", jobrun.ErrAdmissionBlocked, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := durationConditionSpec()
			spec.TimeoutSeconds = 60
			spec.Tasks = append(spec.Tasks,
				v1alpha1.WorkflowTaskSpec{Name: "admit", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "extract-job"}},
				v1alpha1.WorkflowTaskSpec{Name: "running", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "extract-job"}},
			)
			run := walkerRun()
			running := stepFor(t, "running", jobrun.Running, 1)
			running.ID, running.RevisionID, running.SourceUID = 2, 10, "extract-job-uid"
			runs := &fakeWorkflowRuns{
				stored: run, stepErr: tc.admission,
				steps: []workflow.StepRun{stepFor(t, "extract", jobrun.Succeeded, 1), running},
			}
			revs := walkerRevisions(t)
			revs.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, spec))
			pub := &fakeWorkflowPublisher{}
			walker, projected := newWalker(t, runs, revs, pub)
			now := run.CreatedAt.Add(59 * time.Second)
			walker.Now = func() time.Time { return now }
			walker.Workflows = workflowAdmissionClock{runs, func() { now = now.Add(2 * time.Second) }}
			walker.Outcomes = workflowDurationReader(func() (controlplane.TerminalOutcome, error) {
				if len(pub.cancels) != 1+tc.wantCreate {
					t.Fatalf("stop requests were not sent before reading the next duration: %v", pub.cancels)
				}
				return controlplane.TerminalOutcome{StartedAt: run.CreatedAt, CompletedAt: run.CreatedAt.Add(time.Millisecond)}, nil
			})

			n, err := walker.advanceRun(t.Context(), executionTenant, run, definitionIndex(revs.active))
			if err != nil || n != tc.wantCreate {
				t.Fatalf("created=%d want=%d error=%v", n, tc.wantCreate, err)
			}
			if tc.wantCreate == 1 && !pub.published[len(pub.published)-1].Spec.CancelRequested {
				t.Fatal("the committed step was published without its already-observed stop intent")
			}
			if len(*projected) != 1 || (*projected)[0].TaskDecisions["load"].Reason != workflow.ReasonDeadline {
				t.Fatalf("expected the dependent task to stop at the deadline: %+v", *projected)
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

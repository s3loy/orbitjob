package operator

import (
	"context"
	"errors"
	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	"testing"
	"time"
)

func TestWorkflowRepairsCommittedStepAfterPublishFailure(t *testing.T) {
	runs := &fakeWorkflowRuns{stored: walkerRun()}
	revs := walkerRevisions(t)
	pub := &fakeWorkflowPublisher{err: errors.New("temporary API outage")}
	walker, _ := newWalker(t, runs, revs, pub)
	if _, err := walker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runs.steps = append([]workflow.StepRun(nil), runs.createdStep...)
	pub.err = nil
	if _, err := walker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pub.published) != 1 {
		t.Fatalf("committed step missing after recovery: publishes=%d", len(pub.published))
	}
}
func TestWorkflowReferenceStaysInDefinitionNamespace(t *testing.T) {
	runs := &fakeWorkflowRuns{stored: walkerRun()}
	revs := walkerRevisions(t)
	foreign := withIdentity(t, workflowRevision(t, 99, jobSpecJSON(t)), "kubernetes", "foreign-uid", "other-ns", "extract-job")
	revs.active = append(revs.active, foreign)
	revs.byID[99] = foreign
	pub := &fakeWorkflowPublisher{}
	walker, _ := newWalker(t, runs, revs, pub)
	if _, err := walker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pub.published) != 1 || pub.published[0].Namespace != "finance" || pub.published[0].Spec.DefinitionRevision != 10 {
		t.Fatalf("wrong definition: %+v", pub.published)
	}
}

func TestWorkflowDurationConditionUsesMeasuredAttempt(t *testing.T) {
	spec := twoTaskSpec()
	threshold := int64(1000)
	spec.Tasks[1].Condition = &v1alpha1.WorkflowConditionSpec{Type: v1alpha1.ConditionAll, Rules: []v1alpha1.WorkflowRule{{Task: "extract", Field: v1alpha1.RuleFieldDurationMs, Operator: v1alpha1.OperatorLe, Number: &threshold}}}
	revs := walkerRevisions(t)
	revs.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, spec))
	runs := &fakeWorkflowRuns{stored: walkerRun(), steps: []workflow.StepRun{stepFor(t, "extract", jobrun.Succeeded, 1)}}
	pub := &fakeWorkflowPublisher{}
	walker, _ := newWalker(t, runs, revs, pub)
	start := time.Now()
	walker.Outcomes = &fakeTerminalOutcomes{outcome: controlplane.TerminalOutcome{StartedAt: start, CompletedAt: start.Add(2500 * time.Millisecond)}}
	if _, err := walker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runs.createdStep) != 0 {
		t.Fatal("2500 ms attempt passed <=1000ms condition")
	}
	if len(runs.phases) != 1 || runs.phases[0].phase != workflow.PhaseSucceeded {
		t.Fatalf("workflow did not finish after measured condition skip: %+v", runs.phases)
	}
}

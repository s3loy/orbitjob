package operator

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
)

func TestWorkflowManualOnlyStepRepairsFailedPublicationFromPinnedSpec(t *testing.T) {
	runs := &fakeWorkflowRuns{stored: walkerRun()}
	revisions := walkerRevisions(t)
	// Both definitions are manual-only, and the active Job has already changed
	// after this step was committed against the older pinned timeout.
	old := revisions.byID[10]
	raw, err := json.Marshal(v1alpha1.ScheduledJobSpec{JobTemplate: v1alpha1.JobTemplateSpec{Image: "busybox"}, TimeoutSeconds: 11})
	if err != nil {
		t.Fatal(err)
	}
	old.NormalizedSpec = string(raw)
	revisions.byID[10] = old
	for index, active := range revisions.active {
		if active.ID == 10 {
			revisions.active[index] = old
		}
	}
	publisher := &fakeWorkflowPublisher{err: errors.New("initial publication failed")}
	walker, _ := newWalker(t, runs, revisions, publisher)
	if n, err := walker.Tick(context.Background()); err != nil || n != 0 || len(runs.createdStep) != 1 {
		t.Fatalf("failed publish did not leave exactly one committed step: created=%d rows=%d err=%v", n, len(runs.createdStep), err)
	}
	runs.steps = append([]workflow.StepRun{}, runs.createdStep...)
	current := old
	current.ID = 12
	raw, err = json.Marshal(v1alpha1.ScheduledJobSpec{JobTemplate: v1alpha1.JobTemplateSpec{Image: "new-image"}, TimeoutSeconds: 99})
	if err != nil {
		t.Fatal(err)
	}
	current.NormalizedSpec = string(raw)
	for index, active := range revisions.active {
		if active.ID == 10 {
			revisions.active[index] = current
		}
	}
	revisions.byID[12] = current
	publisher.err = nil
	if n, err := walker.Tick(context.Background()); err != nil || n != 0 || len(runs.createdStep) != 1 || len(publisher.published) != 1 {
		t.Fatalf("repair created another step or failed: n=%d rows=%d objects=%d err=%v", n, len(runs.createdStep), len(publisher.published), err)
	}
	got := publisher.published[0].Spec
	if got.DefinitionRevision != 10 || got.TimeoutSeconds != 11 || got.Trigger != v1alpha1.Workflow || got.Actor != ActorWorkflowWalker || got.OccurrenceKey != runs.steps[0].OccurrenceKey {
		t.Fatalf("step repair changed execution identity: %+v", got)
	}
	if runs.steps[0].Phase != jobrun.Pending {
		t.Fatal("publication repaired execution phase instead of its resource")
	}
}

func TestWorkflowDeadlineRepairsUnpublishedStepWithStopIntent(t *testing.T) {
	spec := twoTaskSpec()
	spec.TimeoutSeconds = 60
	revisions := walkerRevisions(t)
	revisions.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, spec))
	run := walkerRun()
	run.Phase = workflow.PhaseRunning
	step := stepFor(t, "extract", jobrun.Pending, 0)
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{step}}
	publisher := &fakeWorkflowPublisher{}
	walker, _ := newWalker(t, runs, revisions, publisher)

	if n, err := walker.Tick(t.Context()); err != nil || n != 0 || len(runs.createdStep) != 0 {
		t.Fatalf("tick at deadline: new steps=%d rows=%+v error=%v", n, runs.createdStep, err)
	}
	if len(publisher.published) != 1 {
		t.Fatalf("expected a repair publication for the committed step: %+v", publisher.published)
	}
	repaired := publisher.published[0]
	if !repaired.Spec.CancelRequested || repaired.Spec.OccurrenceKey != step.OccurrenceKey ||
		repaired.Spec.DefinitionRevision != step.RevisionID || repaired.Spec.ScheduledJobRef.UID != step.SourceUID {
		t.Fatalf("repair must preserve step identity and request the stop on creation: %+v", repaired.Spec)
	}
	if len(publisher.cancels) != 1 || publisher.cancels[0] != repaired.Name {
		t.Fatalf("expected cancellation fanout for an existing step CR: %v", publisher.cancels)
	}
}

type workflowFaultPublisher struct {
	fakeWorkflowPublisher
	publish func(v1alpha1.JobRun) error
	cancel  func(v1alpha1.RunIdentity) error
}

func (p *workflowFaultPublisher) Publish(ctx context.Context, run v1alpha1.JobRun) error {
	if err := p.publish(run); err != nil {
		return err
	}
	return p.fakeWorkflowPublisher.Publish(ctx, run)
}

func (p *workflowFaultPublisher) RequestCancel(ctx context.Context, namespace, name string, identities ...v1alpha1.RunIdentity) error {
	if err := p.cancel(identities[0]); err != nil {
		return err
	}
	return p.fakeWorkflowPublisher.RequestCancel(ctx, namespace, name, identities...)
}

func TestWorkflowDeadlineStopsSiblingDespiteStepErrors(t *testing.T) {
	spec := twoTaskSpec()
	spec.TimeoutSeconds = 60
	spec.Tasks[1].DependsOn = nil
	revisions := walkerRevisions(t)
	revisions.byID[5] = workflowRevision(t, 5, workflowSpecJSON(t, spec))
	run := walkerRun()
	run.Phase = workflow.PhaseRunning
	extract := stepFor(t, "extract", jobrun.Running, 1)
	load := stepFor(t, "load", jobrun.Pending, 0)
	load.ID = 2
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{extract, load}}
	now := run.CreatedAt.Add(59 * time.Second)
	repairErr := errors.New("extract repair failed")
	cancelErr := errors.New("extract cancellation failed")
	publisher := &workflowFaultPublisher{
		publish: func(job v1alpha1.JobRun) error {
			if job.Spec.OccurrenceKey == extract.OccurrenceKey {
				now = run.CreatedAt.Add(61 * time.Second)
				return repairErr
			}
			return nil
		},
		cancel: func(identity v1alpha1.RunIdentity) error {
			if identity.OccurrenceKey == extract.OccurrenceKey {
				return cancelErr
			}
			want := v1alpha1.RunIdentity{SourceUID: load.SourceUID, OccurrenceKey: load.OccurrenceKey, RevisionID: load.RevisionID, Trigger: v1alpha1.Workflow}
			if identity != want {
				t.Fatalf("cancel identity=%+v, want %+v", identity, want)
			}
			return nil
		},
	}
	walker, _ := newWalker(t, runs, revisions, &publisher.fakeWorkflowPublisher)
	walker.Publisher = publisher
	walker.Now = func() time.Time { return now }

	n, err := walker.advanceRun(t.Context(), executionTenant, run, definitionIndex(revisions.active))
	if n != 0 || len(runs.createdStep) != 0 || !errors.Is(err, repairErr) || !errors.Is(err, cancelErr) {
		t.Fatalf("expected both step errors and no new rows: created=%d rows=%+v error=%v", n, runs.createdStep, err)
	}
	if len(publisher.published) != 1 || publisher.published[0].Spec.OccurrenceKey != load.OccurrenceKey || !publisher.published[0].Spec.CancelRequested {
		t.Fatalf("expected sibling repair with stop intent, got %+v", publisher.published)
	}
	want := v1alpha1.RunObjectName("load-job", load.OccurrenceKey)
	if len(publisher.cancels) != 1 || publisher.cancels[0] != want {
		t.Fatalf("expected sibling stop despite earlier failures, got %v", publisher.cancels)
	}
}

package operator

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

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

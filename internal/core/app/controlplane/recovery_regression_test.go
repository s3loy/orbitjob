package controlplane

import (
	"context"
	"testing"
	"time"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
)

type pinnedRevisions struct {
	*fakeRevisions
	old revision.Revision
}

func (r *pinnedRevisions) RevisionByID(context.Context, string, int64) (revision.Revision, error) {
	return r.old, nil
}

type cancelPublisher struct {
	fakePublisher
	canceled []v1alpha1.RunIdentity
}

func (p *cancelPublisher) RequestCancel(_ context.Context, _, _ string, id ...v1alpha1.RunIdentity) error {
	p.canceled = append(p.canceled, id[0])
	return nil
}
func TestRepairUsesPinnedDeadlineAndOriginalTrigger(t *testing.T) {
	current := revisionWith(t, 4, v1alpha1.ScheduledJobSpec{TimeoutSeconds: 90})
	old := revisionWith(t, 3, v1alpha1.ScheduledJobSpec{TimeoutSeconds: 7})
	reader := &pinnedRevisions{fakeRevisions: &fakeRevisions{revisions: []revision.Revision{current}}, old: old}
	runs := &fakeRuns{openRuns: []jobrun.OpenRun{{ID: 42, RevisionID: 3, OccurrenceKey: "old-occurrence", Phase: jobrun.Running, Trigger: jobrun.Function, Actor: "invoker"}}}
	pub := &fakePublisher{}
	s := Scheduler{Revisions: reader, Runs: runs, Publisher: pub}
	if err := s.repairOpenRuns(context.Background(), controlplaneTenant, current); err != nil {
		t.Fatal(err)
	}
	if len(pub.published) != 1 || pub.published[0].Spec.TimeoutSeconds != 7 || pub.published[0].Spec.Trigger != v1alpha1.Function || pub.published[0].Spec.Actor != "invoker" {
		t.Fatalf("repaired mutable spec: %+v", pub.published)
	}
}
func TestReplaceCancelsOldWorkBeforePublishingReplacement(t *testing.T) {
	at := time.Date(2026, 10, 24, 2, 0, 30, 0, time.UTC)
	rev := revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 * * * *", ConcurrencyPolicy: v1alpha1.Replace})
	revs := &fakeRevisions{revisions: []revision.Revision{rev}, openRuns: 1}
	runs := &fakeRuns{openRuns: []jobrun.OpenRun{{ID: 41, RevisionID: 3, OccurrenceKey: "previous-occurrence", Trigger: jobrun.Schedule, Phase: jobrun.Running}}}
	runs.err = &jobrun.AdmissionBlockedError{Policy: "Replace", Reason: "concurrency", CancelTargets: runs.openRuns}
	pub := &cancelPublisher{}
	s := Scheduler{Revisions: revs, Runs: runs, Publisher: pub, Tenants: []string{controlplaneTenant}, Now: func() time.Time { return at }}
	if _, err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pub.canceled) != 1 || len(runs.recorded) != 0 {
		t.Fatalf("old work not stopped first: cancel=%v created=%v", pub.canceled, runs.recorded)
	}
	revs.openRuns = 0
	runs.openRuns = nil
	runs.err = nil
	if _, err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runs.recorded) != 1 || !runs.recorded[0].run.ScheduledFor.Equal(at.Truncate(time.Minute)) {
		t.Fatalf("replacement occurrence time lost: %+v", runs.recorded)
	}
}

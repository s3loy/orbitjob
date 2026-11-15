package controlplane

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
)

type pinnedRepairRevisions struct {
	*fakeRevisions
	pinned map[int64]revision.Revision
}

func (f pinnedRepairRevisions) RevisionByID(_ context.Context, _ string, id int64) (revision.Revision, error) {
	rev, ok := f.pinned[id]
	if !ok {
		return revision.Revision{}, fmt.Errorf("missing revision")
	}
	return rev, nil
}

func TestScheduleRepairPreservesOriginalOriginActorAndPinnedTimeout(t *testing.T) {
	for _, origin := range []jobrun.Trigger{jobrun.Manual, jobrun.Workflow, jobrun.Schedule} {
		t.Run(string(origin), func(t *testing.T) {
			old := revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 3 * * *", TimeoutSeconds: 11})
			current := revisionWith(t, 4, v1alpha1.ScheduledJobSpec{Schedule: "0 3 * * *", TimeoutSeconds: 99})
			revisions := pinnedRepairRevisions{fakeRevisions: &fakeRevisions{revisions: []revision.Revision{current}}, pinned: map[int64]revision.Revision{3: old}}
			runs := &fakeRuns{openRuns: []jobrun.OpenRun{{ID: 7, RevisionID: 3, OccurrenceKey: "original-full-occurrence", Phase: jobrun.CancelRequested, Trigger: origin, Actor: "original-actor"}}}
			publisher := &fakePublisher{}
			scheduler := Scheduler{Revisions: revisions, Runs: runs, Publisher: publisher, Tenants: []string{"tenant-a"}, Now: func() time.Time { return time.Date(2026, 3, 1, 2, 0, 30, 0, time.UTC) }}
			if n, err := scheduler.Tick(context.Background()); err != nil || n != 0 {
				t.Fatalf("tick=%d err=%v", n, err)
			}
			if len(publisher.published) != 1 {
				t.Fatalf("repairs=%d", len(publisher.published))
			}
			got := publisher.published[0].Spec
			if got.DefinitionRevision != 3 || got.Trigger != v1alpha1.Trigger(origin) || got.Actor != "original-actor" || got.TimeoutSeconds != 11 || !got.CancelRequested || got.OccurrenceKey != "original-full-occurrence" {
				t.Fatalf("repair changed pinned execution: %+v", got)
			}
		})
	}
}

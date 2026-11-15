package operator

import (
	"context"
	"testing"
	"time"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	corepostgres "orbitjob/internal/core/store/postgres"
)

type cancelAtReservation struct {
	*fakeRuns
	reserved bool
}

type workflowStopsAfterReservation struct{ *fakeRuns }

func (s *workflowStopsAfterReservation) CreateAttemptWithEnsure(_ context.Context, _ string, _ int64, attempt jobrun.Attempt, _ func(context.Context) (jobrun.Attempt, error)) error {
	// The first transaction committed, but the parent stop won before Ensure.
	// A previous transport attempt could be ambiguous, so keep the UID unknown.
	s.stored.Attempt = attempt.Number
	s.stored.Phase = jobrun.CreatingAttempt
	s.stored.AttemptJobName = attempt.KubernetesJobName
	return corepostgres.ErrWorkflowStopped
}

func TestParentStopReloadsUncertainIntentAndRetriesWithDelay(t *testing.T) {
	obj := jobRunObject(t)
	runs := &workflowStopsAfterReservation{fakeRuns: &fakeRuns{found: true, reportPhaseChange: true, stored: jobrun.StoredRun{ID: 42, RevisionID: 7, Phase: jobrun.Pending, MaxAttempts: 1}}}
	jobs := &fakeJobs{}
	revs := &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, pinnedSpec)}}
	rt := newRuntime(t, fakeDynamic(t, &obj), revs, runs, jobs)
	immediate := 0
	var delay time.Duration
	rt.Enqueue = func(string, string, string) { immediate++ }
	rt.EnqueueAfter = func(_ string, _ string, _ string, after time.Duration) { delay = after }
	if err := rt.ReconcileJobRun(t.Context(), obj); err != nil {
		t.Fatal(err)
	}
	if len(jobs.created) != 0 || immediate != 0 || delay != 5*time.Second {
		t.Fatalf("parent stop executed or hot-looped: created=%d immediate=%d delay=%s", len(jobs.created), immediate, delay)
	}
	if len(runs.phases) == 0 || runs.phases[len(runs.phases)-1].phase != jobrun.CancelUnknown {
		t.Fatalf("uncertain intent was falsely declared stopped: %+v", runs.phases)
	}
}

func (s *cancelAtReservation) CreateAttemptWithEnsure(_ context.Context, _ string, _ int64, attempt jobrun.Attempt, _ func(context.Context) (jobrun.Attempt, error)) error {
	// A cancel wins after a durable reservation but before the locked Ensure.
	s.reserved = attempt.Phase == jobrun.CreatingAttempt && attempt.KubernetesJobUID == "" && attempt.KubernetesJobName != ""
	s.stored.Attempt = attempt.Number
	s.stored.Phase = jobrun.CancelRequested
	return corepostgres.ErrRunChanged
}
func TestCancellationWinningCreationFenceNeverStartsJob(t *testing.T) {
	obj := jobRunObject(t)
	runs := &cancelAtReservation{fakeRuns: &fakeRuns{found: true, stored: jobrun.StoredRun{ID: 42, RevisionID: 7, Phase: jobrun.Pending, MaxAttempts: 1}}}
	jobs := &fakeJobs{}
	revs := &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, pinnedSpec)}}
	rt := newRuntime(t, fakeDynamic(t, &obj), revs, runs, jobs)
	woken := false
	rt.Enqueue = func(resource, ns, name string) { woken = resource == "jobruns" && name == obj.GetName() }
	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if !runs.reserved || len(jobs.created) != 0 || !woken {
		t.Fatalf("cancel/create fence broken: reserved=%v Jobs=%d wake=%v", runs.reserved, len(jobs.created), woken)
	}
}

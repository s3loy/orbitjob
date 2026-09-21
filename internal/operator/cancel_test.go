package operator

import (
	"context"
	"errors"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"orbitjob/internal/core/domain/jobrun"
)

// jobRunObjectWithCancel renders a JobRun whose owner asked for a stop.
func jobRunObjectWithCancel(t *testing.T, phase string, attempt int) (unstructured.Unstructured, jobrun.StoredRun) {
	t.Helper()
	obj := jobRunObject(t)
	spec := obj.Object["spec"].(map[string]any)
	spec["cancelRequested"] = true
	stored := jobrun.StoredRun{
		ID: 11, RevisionID: 7, Phase: jobrun.Phase(phase), Attempt: attempt, MaxAttempts: 3, Epoch: 4,
	}
	return obj, stored
}

func TestCancelWritesIntentThenConfirmsAfterJobIsGone(t *testing.T) {
	obj, stored := jobRunObjectWithCancel(t, string(jobrun.Running), 1)
	runs := &fakeRuns{found: true, stored: stored}
	jobs := &fakeJobs{}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}

	// The intent is recorded before the Job is touched, so a crash between the
	// two leaves a durable request rather than a forgotten stop.
	if len(runs.phases) == 0 || runs.phases[0].phase != jobrun.CancelRequested {
		t.Fatalf("first write = %+v, want CancelRequested", runs.phases)
	}
	if len(jobs.deleted) != 1 || jobs.deleted[0] != "oj-nightly-report-1-1" {
		t.Fatalf("deleted = %v", jobs.deleted)
	}
	// The Job is gone by the time the delete returns, so the same reconcile can
	// confirm the stop instead of waiting a whole tick.
	last := runs.phases[len(runs.phases)-1]
	if last.phase != jobrun.Canceled {
		t.Fatalf("final phase = %+v, want Canceled", runs.phases)
	}
}

func TestCancelWaitsWhileJobIsTerminating(t *testing.T) {
	obj, stored := jobRunObjectWithCancel(t, string(jobrun.Running), 1)
	// Deletion started moments ago: Kubernetes has not finished reclaiming the
	// Pods, so the platform cannot yet claim the workload stopped.
	runs := &fakeRuns{found: true, stored: stored}
	jobs := &fakeJobs{terminationGrace: time.Minute}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(jobs.deleted) != 1 {
		t.Fatalf("delete calls = %v", jobs.deleted)
	}
	for _, write := range runs.phases {
		if write.phase == jobrun.Canceled {
			t.Fatal("must not confirm a cancel while the job is still terminating")
		}
	}
}

func TestCancelReportsUnknownWhenDeletionNeverCompletes(t *testing.T) {
	obj, stored := jobRunObjectWithCancel(t, string(jobrun.CancelRequested), 1)
	// Deletion has been in flight far longer than the confirmation timeout, so
	// the platform admits it cannot prove the workload stopped.
	runs := &fakeRuns{found: true, stored: stored}
	stuck := &fakeJobs{terminationGrace: 24 * time.Hour}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, stuck)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(runs.phases) != 1 || runs.phases[0].phase != jobrun.CancelUnknown {
		t.Fatalf("phases = %+v, want a single CancelUnknown", runs.phases)
	}
	if stuck.terminatingAt == nil {
		t.Fatal("the job must still be observed as terminating")
	}
}

func TestCancelWithNoAttemptIsImmediatelyComplete(t *testing.T) {
	obj, stored := jobRunObjectWithCancel(t, string(jobrun.Pending), 0)
	runs := &fakeRuns{found: true, stored: stored}
	jobs := &fakeJobs{}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(jobs.deleted) != 0 {
		t.Fatal("nothing was started, so nothing should be deleted")
	}
	if len(runs.phases) != 1 || runs.phases[0].phase != jobrun.Canceled {
		t.Fatalf("phases = %+v", runs.phases)
	}
}

func TestCancelDoesNotStartAnotherAttempt(t *testing.T) {
	obj, stored := jobRunObjectWithCancel(t, string(jobrun.RetryWaiting), 1)
	runs := &fakeRuns{found: true, stored: stored}
	jobs := &fakeJobs{}
	revisions := &fakeRevisions{}
	rt := newRuntime(t, fakeDynamic(t, &obj), revisions, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(jobs.created) != 0 {
		t.Fatal("a cancelled run must not create a job")
	}
	// The revision is only needed to build a job; a cancel must not even look
	// one up, so a missing revision cannot block stopping a run.
	if len(revisions.applied) != 0 {
		t.Fatal("cancel must not project a revision")
	}
}

func TestCancelPropagatesDeleteFailure(t *testing.T) {
	obj, stored := jobRunObjectWithCancel(t, string(jobrun.Running), 1)
	runs := &fakeRuns{found: true, stored: stored}
	jobs := &fakeJobs{deleteErr: errors.New("api down")}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err == nil {
		t.Fatal("a failed delete must surface so the run keeps retrying")
	}
	for _, write := range runs.phases {
		if write.phase == jobrun.Canceled {
			t.Fatal("must not confirm a cancel whose delete failed")
		}
	}
}

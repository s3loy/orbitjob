package operator

import (
	"context"
	"errors"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/jobrun"
	revision "orbitjob/internal/core/domain/revision"
)

const pinnedSpec = `{"schedule":"0 2 * * *","jobTemplate":{"image":"registry.example.com/report:v1","args":["--slow"],"backoffLimit":2}}`

func TestReconcileScheduledJobProjectsRevisionAndStatus(t *testing.T) {
	obj := scheduledJobObject(t)
	dyn := fakeDynamic(t, &obj)
	revisions := &fakeRevisions{}
	rt := newRuntime(t, dyn, revisions, &fakeRuns{}, &fakeJobs{})

	if err := rt.ReconcileScheduledJob(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(revisions.applied) != 1 {
		t.Fatalf("applied %d revisions, want 1", len(revisions.applied))
	}
	if revisions.applied[0].Identity.SourceUID != "uid-1" || revisions.applied[0].Generation != 3 {
		t.Fatalf("unexpected revision identity: %+v", revisions.applied[0].Identity)
	}

	stored, err := dyn.Resource(scheduledJobGVR).Namespace("finance").Get(context.Background(), "nightly-report", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	status, _, _ := unstructured.NestedMap(stored.Object, "status")
	if status["observedGeneration"] != int64(3) {
		t.Errorf("observedGeneration = %v", status["observedGeneration"])
	}
	if status["activeRevision"] != int64(1) {
		t.Errorf("activeRevision = %v", status["activeRevision"])
	}
}

func TestReconcileScheduledJobSurfacesProjectionErrors(t *testing.T) {
	obj := scheduledJobObject(t)
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{applyErr: errors.New("conflict")}, &fakeRuns{}, &fakeJobs{})
	if err := rt.ReconcileScheduledJob(context.Background(), obj); err == nil {
		t.Fatal("expected projection error to surface")
	}

	unmapped := scheduledJobObject(t)
	unmapped.SetNamespace("unknown")
	rt2 := newRuntime(t, fakeDynamic(t, &unmapped), &fakeRevisions{}, &fakeRuns{}, &fakeJobs{})
	if err := rt2.ReconcileScheduledJob(context.Background(), unmapped); err == nil {
		t.Fatal("expected unmapped namespace to be rejected")
	}
}

func TestReconcileJobRunCreatesFirstAttempt(t *testing.T) {
	obj := jobRunObject(t)
	dyn := fakeDynamic(t, &obj)
	runs := &fakeRuns{
		found: true,
		stored: jobrun.StoredRun{
			ID: 11, RevisionID: 7, Phase: jobrun.Pending, Attempt: 0, MaxAttempts: 2, Epoch: 4,
		},
	}
	jobs := &fakeJobs{}
	revisions := &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, pinnedSpec)}}
	rt := newRuntime(t, dyn, revisions, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}

	desired := jobs.lastCreated()
	if desired == nil {
		t.Fatal("no kubernetes job created")
	}
	if desired.Name != "oj-nightly-report-1-1" {
		t.Fatalf("job name = %q", desired.Name)
	}
	if desired.Labels[execution.LabelRevisionID] != "7" || desired.Labels[execution.LabelRunUID] != "run-uid-1" {
		t.Fatalf("job labels = %v", desired.Labels)
	}
	// The pinned revision, not the current spec, decides what runs.
	if got := desired.Spec.Template.Spec.Containers[0].Image; got != "registry.example.com/report:v1" {
		t.Fatalf("image = %q", got)
	}

	if len(runs.attempts) != 1 {
		t.Fatalf("recorded %d attempts, want 1", len(runs.attempts))
	}
	// The attempt is recorded with the run advancing to Running in the same
	// store call, so an observed re-delivery cannot start a second attempt.
	recorded := runs.attempts[0]
	if recorded.attempt.Number != 1 || recorded.attempt.KubernetesJobUID != "k8s-uid-1" || recorded.runPhase != jobrun.Running {
		t.Fatalf("recorded attempt = %+v", recorded)
	}
	// The store advances the run to Running inside the attempt's own transaction,
	// so no separate phase write may follow the attempt write.
	if len(runs.phases) != 0 {
		t.Fatalf("run phases = %+v, want none outside the attempt transaction", runs.phases)
	}
}

func TestReconcileJobRunStartsNextAttemptWhenRetrying(t *testing.T) {
	obj := jobRunObject(t)
	runs := &fakeRuns{
		found: true,
		stored: jobrun.StoredRun{
			ID: 11, RevisionID: 7, Phase: jobrun.RetryWaiting, Attempt: 1, MaxAttempts: 2, Epoch: 4,
		},
	}
	jobs := &fakeJobs{}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, pinnedSpec)}}, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if got := jobs.lastCreated(); got == nil || got.Name != "oj-nightly-report-1-2" {
		t.Fatalf("second attempt job = %v", got)
	}
	if len(runs.attempts) != 1 || runs.attempts[0].attempt.Number != 2 {
		t.Fatalf("attempts = %+v", runs.attempts)
	}
}

func TestReconcileJobRunFailsWhenAttemptsExhausted(t *testing.T) {
	obj := jobRunObject(t)
	runs := &fakeRuns{
		found: true,
		stored: jobrun.StoredRun{
			ID: 11, RevisionID: 7, Phase: jobrun.RetryWaiting, Attempt: 2, MaxAttempts: 2, Epoch: 4,
		},
	}
	jobs := &fakeJobs{}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, pinnedSpec)}}, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(jobs.created) != 0 {
		t.Fatal("exhausted run must not create another job")
	}
	if len(runs.phases) != 1 || runs.phases[0].phase != jobrun.Failed {
		t.Fatalf("run phases = %+v", runs.phases)
	}
}

func TestReconcileJobRunIsNoOpForTerminalRun(t *testing.T) {
	obj := jobRunObject(t)
	runs := &fakeRuns{
		found: true,
		stored: jobrun.StoredRun{
			ID: 11, RevisionID: 7, Phase: jobrun.Succeeded, Attempt: 1, MaxAttempts: 2, Epoch: 4,
		},
	}
	jobs := &fakeJobs{}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(jobs.created) != 0 || len(runs.phases) != 0 {
		t.Fatal("terminal run must not be driven further")
	}
}

func TestReconcileJobRunIsNoOpForAnOrphanOccurrence(t *testing.T) {
	// found=false on a non-manual trigger means the row existed once and was
	// taken away (retention, or a tenant-blind loadtest reset). The refusal
	// stance stays -- the operator must not materialize platform state for a
	// CR the scheduler owns -- but the key is dropped instead of requeued,
	// because no future event can change the answer.
	obj := jobRunObject(t)
	runs := &fakeRuns{found: false}
	jobs := &fakeJobs{}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatalf("an orphaned occurrence must be dropped, got %v", err)
	}
	if len(jobs.created) != 0 || len(runs.phases) != 0 {
		t.Fatal("an orphaned occurrence must not gain platform state")
	}
}

func TestReconcileJobRunRefusesJobWithoutUID(t *testing.T) {
	obj := jobRunObject(t)
	runs := &fakeRuns{
		found:  true,
		stored: jobrun.StoredRun{ID: 11, RevisionID: 7, Phase: jobrun.Pending, MaxAttempts: 1, Epoch: 4},
	}
	jobs := &fakeJobs{job: &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "oj-nightly-report-1-1"}}}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, pinnedSpec)}}, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err == nil {
		t.Fatal("a job without UID must not be recorded as owned")
	}
	if len(runs.attempts) != 0 {
		t.Fatal("no attempt may be recorded without a verifiable job UID")
	}
}

func observedJob(t *testing.T, name string, phase string) unstructured.Unstructured {
	t.Helper()
	conditions := []any{}
	switch phase {
	case execution.PhaseSucceeded:
		conditions = []any{map[string]any{"type": "Complete", "status": "True"}}
	case execution.PhaseFailed:
		conditions = []any{map[string]any{"type": "Failed", "status": "True"}}
	}
	obj := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name": name, "namespace": "finance", "uid": "k8s-uid-1", "resourceVersion": "150",
			"labels": map[string]any{
				execution.LabelRunName:    "nightly-report-1",
				execution.LabelRunUID:     "run-uid-1",
				execution.LabelRevisionID: "7",
				execution.LabelAttempt:    "1",
			},
		},
		"status": map[string]any{"conditions": conditions},
	}}
	obj.SetGroupVersionKind(jobGVR.GroupVersion().WithKind("Job"))
	return obj
}

func TestReconcileJobProjectsObservedPhase(t *testing.T) {
	tests := []struct {
		name      string
		jobPhase  string
		wantPhase jobrun.Phase
	}{
		{"success completes the run", execution.PhaseSucceeded, jobrun.Succeeded},
		// A failed Job parks in RetryWaiting: only the run reconciler knows
		// whether attempts remain.
		{"failure waits for retry decision", execution.PhaseFailed, jobrun.RetryWaiting},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := observedJob(t, "oj-nightly-report-1-1", tt.jobPhase)
			runs := &fakeRuns{observedRun: 11, observedNum: 1}
			rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, &fakeJobs{})

			if err := rt.ReconcileJob(context.Background(), obj); err != nil {
				t.Fatal(err)
			}
			if len(runs.phases) != 1 {
				t.Fatalf("run phase writes = %+v", runs.phases)
			}
			if runs.phases[0].phase != tt.wantPhase || runs.phases[0].runID != 11 || runs.phases[0].attempt != 1 {
				t.Fatalf("run phase write = %+v", runs.phases[0])
			}
		})
	}
}

func TestReconcileJobIgnoresUnmanagedJob(t *testing.T) {
	obj := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata":   map[string]any{"name": "someone-elses", "namespace": "finance", "uid": "x"},
	}}
	obj.SetGroupVersionKind(jobGVR.GroupVersion().WithKind("Job"))
	runs := &fakeRuns{observedRun: 11, observedNum: 1}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, &fakeJobs{})

	if err := rt.ReconcileJob(context.Background(), obj); err != nil {
		t.Fatalf("unmanaged job must not error: %v", err)
	}
	if len(runs.phases) != 0 {
		t.Fatal("unmanaged job must not write run state")
	}
}

func TestReconcileJobSurfacesUnknownAttempt(t *testing.T) {
	obj := observedJob(t, "oj-nightly-report-1-9", execution.PhaseSucceeded)
	runs := &fakeRuns{observedErr: errors.New("no attempt")}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, &fakeJobs{})
	if err := rt.ReconcileJob(context.Background(), obj); err == nil {
		t.Fatal("an observation with no owning attempt must surface")
	}
}

func TestRunPhaseMappingCoversEveryJobPhase(t *testing.T) {
	if got := executionMapToRunPhase(execution.PhasePending); got != jobrun.CreatingAttempt {
		t.Fatalf("pending mapped to %s", got)
	}
	if got := executionMapToRunPhase(execution.PhaseRunning); got != jobrun.Running {
		t.Fatalf("running mapped to %s", got)
	}
}

// TestReconcileJobRunRefusesAForeignJob covers the adoption path. Ensure is
// idempotent by namespace and name, so a Job already carrying this name is
// adopted rather than recreated. Name is not proof of ownership: a run name
// repeats when a ScheduledJob is deleted and recreated, and a Job left over
// from before that can still be present. Recording its UID would make this run
// drive a workload it does not own, so the labels are checked first.
// TestReconcileJobRunDoesNotStartASecondAttemptWhileRunning is the regression
// test for the critical defect. The run row moves to Running the moment the
// attempt starts, and the status patch that records it re-delivers the JobRun to
// this same reconcile loop. Reading Running as permission to start another
// attempt makes one occurrence execute its job body concurrently.
func TestReconcileJobRunDoesNotStartASecondAttemptWhileRunning(t *testing.T) {
	obj := jobRunObject(t)
	runs := &fakeRuns{
		found: true,
		stored: jobrun.StoredRun{
			ID: 11, RevisionID: 7, Phase: jobrun.Running, Attempt: 1, MaxAttempts: 3, Epoch: 4,
		},
	}
	jobs := &fakeJobs{}
	revisions := &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, pinnedSpec)}}
	rt := newRuntime(t, fakeDynamic(t, &obj), revisions, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(jobs.created) != 0 {
		t.Fatalf("started %d job(s) while attempt 1 was still running", len(jobs.created))
	}
	if len(runs.attempts) != 0 {
		t.Fatalf("recorded attempts against an in-flight run: %+v", runs.attempts)
	}
	if len(runs.phases) != 0 {
		t.Fatalf("wrote a run phase for an in-flight run: %+v", runs.phases)
	}
}

// TestReconcileJobRunDoesNotStartAnAttemptWhileOneIsBeingCreated covers the same
// hole from the other phase an attempt can be in. An observed Pending Job is
// recorded as CreatingAttempt, and the Job exists, so starting another attempt
// would again run the occurrence twice.
func TestReconcileJobRunDoesNotStartAnAttemptWhileOneIsBeingCreated(t *testing.T) {
	obj := jobRunObject(t)
	runs := &fakeRuns{
		found: true,
		stored: jobrun.StoredRun{
			ID: 11, RevisionID: 7, Phase: jobrun.CreatingAttempt, Attempt: 1, MaxAttempts: 3, Epoch: 4,
		},
	}
	jobs := &fakeJobs{}
	revisions := &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, pinnedSpec)}}
	rt := newRuntime(t, fakeDynamic(t, &obj), revisions, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(jobs.created) != 0 || len(runs.attempts) != 0 || len(runs.phases) != 0 {
		t.Fatalf("drove an in-flight run: jobs=%d attempts=%d phases=%+v",
			len(jobs.created), len(runs.attempts), runs.phases)
	}
}

// TestReconcileJobRunDoesNotFailARunningRunAtTheAttemptLimit covers the other
// half of the defect. With the default maxAttempts of 1 the "attempts
// exhausted" branch fires while the only attempt is still executing, recording
// Failed against a Job that has not finished. Recording an outcome is only legal
// when no attempt is in flight.
func TestReconcileJobRunDoesNotFailARunningRunAtTheAttemptLimit(t *testing.T) {
	obj := jobRunObject(t)
	runs := &fakeRuns{
		found: true,
		stored: jobrun.StoredRun{
			ID: 11, RevisionID: 7, Phase: jobrun.Running, Attempt: 1, MaxAttempts: 1, Epoch: 4,
		},
	}
	jobs := &fakeJobs{}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, jobs)

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(jobs.created) != 0 {
		t.Fatal("no new job may start while attempt 1 is running")
	}
	for _, write := range runs.phases {
		if write.phase == jobrun.Failed {
			t.Fatal("a run with an attempt in flight was recorded Failed")
		}
	}
}

// TestReconcileJobIgnoresAnObservationOfAFinishedRun covers the store's refusal
// to move a terminal run. The refusal is the guard working, not a reconcile
// failure: Kubernetes Jobs survive their run and the informer re-delivers them,
// so surfacing the error would retry the same doomed write forever. The fake
// supplies the refusal; the assertion is on the operator's handling of it.
func TestReconcileJobIgnoresAnObservationOfAFinishedRun(t *testing.T) {
	obj := observedJob(t, "oj-nightly-report-1-1", execution.PhaseFailed)
	runs := &fakeRuns{observedRun: 11, observedNum: 1, phaseErr: jobrun.ErrRunTerminal}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, &fakeJobs{})

	if err := rt.ReconcileJob(context.Background(), obj); err != nil {
		t.Fatalf("a phase write refused for a finished run must not fail the reconcile: %v", err)
	}
}

func TestReconcileJobRunRefusesAForeignJob(t *testing.T) {
	obj := jobRunObject(t)
	dyn := fakeDynamic(t, &obj)
	runs := &fakeRuns{
		found: true,
		stored: jobrun.StoredRun{
			ID: 11, RevisionID: 7, Phase: jobrun.Pending, Attempt: 0, MaxAttempts: 2, Epoch: 4,
		},
	}
	jobs := &fakeJobs{job: &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name:      "oj-nightly-report-1-1",
		Namespace: "finance",
		UID:       "someone-elses-uid",
		Labels: map[string]string{
			execution.LabelRunName:    "a-different-run",
			execution.LabelRunUID:     "a-different-uid",
			execution.LabelAttempt:    "1",
			execution.LabelRevisionID: "7",
		},
	}}}
	revisions := &fakeRevisions{byID: map[int64]revision.Revision{7: revisionFor(t, 7, pinnedSpec)}}
	rt := newRuntime(t, dyn, revisions, runs, jobs)

	err := rt.ReconcileJobRun(context.Background(), obj)
	if err == nil {
		t.Fatal("a job belonging to another run was adopted")
	}
	if !errors.Is(err, execution.ErrForeignJob) {
		t.Fatalf("expected ErrForeignJob, got %v", err)
	}
	if len(runs.attempts) != 0 {
		t.Fatalf("recorded %d attempts against a foreign job", len(runs.attempts))
	}
	if len(runs.phases) != 0 {
		t.Fatalf("advanced the run against a foreign job: %+v", runs.phases)
	}
}

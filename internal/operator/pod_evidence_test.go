package operator

import (
	"context"
	"errors"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"orbitjob/internal/core/domain/jobrun"
)

func TestRetainedCancellationGraceUsesPersistedClockAcrossRestart(t *testing.T) {
	r, s, j, obj, spec := retainedCancellationFixture(t)
	start := s.e.PhaseUpdatedAt
	now := start.Add(4 * time.Minute)
	r.Now = func() time.Time { return now }
	for range 2 {
		restarted := r
		if err := restarted.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
			t.Fatal(err)
		}
		if s.stored.Phase != jobrun.CancelRequested || !j.held(t) || !s.e.PhaseUpdatedAt.Equal(start) {
			t.Fatal("grace period lost hold, changed phase or restarted clock")
		}
	}
	if j.stopRequests != 1 || len(j.deleted) != 0 {
		t.Fatal("repeated observations repeated the stop request or deleted unproven evidence")
	}
	now = start.Add(5 * time.Minute)
	if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.CancelUnknown || s.stored.UnknownSince != nil || !j.held(t) {
		t.Fatal("pure timeout invented a permanent evidence gap or released hold")
	}
	j.terminate(t)
	if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.Canceled || !jobrun.EvidenceReleaseComplete(s.stored.TerminationEvidence) || j.held(t) {
		t.Fatal("complete retained inventory could not finish after timeout")
	}
}

func TestRetainedCancellationDatabaseFailureRetainsExitEvidence(t *testing.T) {
	r, s, j, obj, spec := retainedCancellationFixture(t)
	j.terminate(t)
	s.recordErr = errors.New("database unavailable")
	if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); !errors.Is(err, s.recordErr) {
		t.Fatal("proof write failure was lost")
	}
	if !j.held(t) || s.stored.Phase != jobrun.CancelRequested || j.job.DeletionTimestamp != nil {
		t.Fatal("stop request lost retained evidence during database outage")
	}
	s.recordErr = nil
	j.beforeRelease = func() {
		if !jobrun.HasRetainedTerminationEvidence(s.stored.TerminationEvidence, string(j.job.UID)) {
			t.Fatal("hold released before durable complete proof")
		}
	}
	if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.Canceled || j.held(t) || s.proofWrites != 1 {
		t.Fatal("database recovery failed to settle original attempt once")
	}
}

func TestRetainedCancellationRestartsAtReleaseCheckpoints(t *testing.T) {
	for _, point := range []string{"before-release", "after-release"} {
		t.Run(point, func(t *testing.T) {
			r, s, j, obj, spec := retainedCancellationFixture(t)
			j.terminate(t)
			failure := errors.New("injected outage")
			if point == "before-release" {
				j.releaseErr = failure
			} else {
				s.releasedErr = failure
			}
			if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); !errors.Is(err, failure) {
				t.Fatalf("checkpoint failure=%v", err)
			}
			if s.stored.Phase != jobrun.CancelRequested || !jobrun.EvidenceReleasePending(s.stored.TerminationEvidence) {
				t.Fatal("failed checkpoint advanced run or discarded pending proof")
			}
			original := append([]byte(nil), s.stored.TerminationEvidence...)
			j.releaseErr, s.releasedErr = nil, nil
			restarted := r
			if err := restarted.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
				t.Fatal(err)
			}
			if s.proofWrites != 1 || len(original) == 0 || s.stored.Phase != jobrun.Canceled || j.held(t) {
				t.Fatal("restart replaced complete proof, repeated attempt or stranded hold")
			}
		})
	}
}

func TestRetainedCancellationGapCannotBeReplacedByLaterHealthySample(t *testing.T) {
	for _, gap := range []string{"NodeLost", "ContainerStatusUnknown", "missing", "replaced"} {
		t.Run(gap, func(t *testing.T) {
			r, s, j, obj, spec := retainedCancellationFixture(t)
			j.terminate(t)
			pod := j.pod(t)
			switch gap {
			case "NodeLost":
				pod.Status.Reason = "NodeLost"
			case "ContainerStatusUnknown":
				pod.Status.ContainerStatuses[0].State.Terminated.Reason = gap
			case "missing":
				pod.Finalizers = nil
			case "replaced":
				j.job.UID = "replacement-job"
			}
			if _, err := j.client.CoreV1().Pods("finance").Update(t.Context(), pod, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
				t.Fatal(err)
			}
			if s.stored.Phase != jobrun.CancelUnknown || s.stored.UnknownSince == nil || s.isolations == 0 || s.proofWrites != 0 {
				t.Fatal("first evidence gap was not durably isolated")
			}
			j.job.UID = "original-job"
			j.terminate(t)
			if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
				t.Fatal(err)
			}
			if s.stored.Phase != jobrun.CancelUnknown || s.proofWrites != 0 || s.releases != 0 {
				t.Fatal("later sample erased an evidence gap")
			}
		})
	}
}

func TestRetainedCancellationWaitsForForeignJobFinalizer(t *testing.T) {
	r, s, j, obj, spec := retainedCancellationFixture(t)
	j.terminate(t)
	j.foreignHold = true
	s.e.PhaseUpdatedAt = r.now().Add(-6 * time.Minute)
	if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.CancelUnknown || !jobrun.EvidenceReleaseComplete(s.stored.TerminationEvidence) || j.held(t) {
		t.Fatal("foreign Job hold was treated as cancellation completion")
	}
	j.gone = true
	if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.Canceled {
		t.Fatal("proven stop could not complete after foreign hold removal")
	}
}

func TestNormalFailureTargetReleasesEvidenceBeforeFinalPhase(t *testing.T) {
	r, s, j, _, _ := retainedCancellationFixture(t)
	s.stored.Phase = jobrun.Running
	j.terminate(t)
	j.job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue}}
	observe := func() {
		t.Helper()
		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(j.job)
		if err != nil {
			t.Fatal(err)
		}
		if err = r.ReconcileJob(t.Context(), unstructured.Unstructured{Object: raw}); err != nil {
			t.Fatal(err)
		}
	}
	observe()
	if j.held(t) || !jobrun.EvidenceReleaseComplete(s.stored.TerminationEvidence) || s.stored.Phase != jobrun.Running {
		t.Fatal("FailureTarget deadlocked evidence or prematurely completed the run")
	}
	j.job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
	observe()
	if s.stored.AttemptPhase != jobrun.Failed || s.stored.Phase != jobrun.RetryWaiting || s.proofWrites != 1 {
		t.Fatal("final Job failure did not advance through already released evidence")
	}
}

func TestPendingEvidenceRecoveryIncludesHistoricalTerminalAttempt(t *testing.T) {
	r, s, j, _, _ := retainedCancellationFixture(t)
	j.terminate(t)
	j.job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	proof, complete, err := j.adapter.InspectRetainedTermination(t.Context(), j.job, nil)
	if err != nil || !complete {
		t.Fatal(err)
	}
	s.stored.TerminationEvidence = proof
	s.stored.Phase, s.stored.Attempt = jobrun.Succeeded, 2
	s.targetNumber = 1
	cursor := &ExecutionRecoveryCursor{}
	if _, err = r.RecoverExecutions(t.Context(), []string{executionTenant}, 10, cursor); err != nil {
		t.Fatal(err)
	}
	if j.held(t) || !jobrun.EvidenceReleaseComplete(s.stored.TerminationEvidence) || s.stored.Attempt != 2 || s.stored.Phase != jobrun.Succeeded || len(s.phases) != 0 {
		t.Fatal("historical cleanup advanced current run or stranded the old hold")
	}
}

func TestCancellationObservationOutageCannotExtendConfirmationBudget(t *testing.T) {
	for _, elapsed := range []time.Duration{4 * time.Minute, 6 * time.Minute} {
		r, s, j, obj, spec := retainedCancellationFixture(t)
		s.e.PhaseUpdatedAt = r.now().Add(-elapsed)
		failure := errors.New("apiserver unavailable")
		j.err = failure
		if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); !errors.Is(err, failure) {
			t.Fatalf("original observation error lost: %v", err)
		}
		want := jobrun.CancelRequested
		if elapsed > 5*time.Minute {
			want = jobrun.CancelUnknown
		}
		if s.stored.Phase != want || s.stored.UnknownSince != nil || !j.held(t) {
			t.Fatal("transport failure extended budget or invented evidence isolation")
		}
	}
}

func TestCancellationTimeoutDoesNotClaimFailedDatabaseWrite(t *testing.T) {
	r, s, j, obj, spec := retainedCancellationFixture(t)
	s.e.PhaseUpdatedAt = r.now().Add(-6 * time.Minute)
	observation := errors.New("API unavailable")
	database := errors.New("database unavailable")
	j.err, s.phaseErr = observation, database
	err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec)
	if !errors.Is(err, observation) || !errors.Is(err, database) || s.stored.Phase != jobrun.CancelRequested || s.stored.UnknownSince != nil {
		t.Fatal("failed timeout write lost its errors or claimed an uncommitted phase")
	}
}

func TestCanceledObservationContextDoesNotStartAnotherWrite(t *testing.T) {
	r, s, _, _, _ := retainedCancellationFixture(t)
	s.e.PhaseUpdatedAt = r.now().Add(-6 * time.Minute)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	failure := errors.New("observation interrupted")
	if err := r.cancellationObservationFailed(ctx, executionTenant, s.e, failure); !errors.Is(err, failure) || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation context was lost")
	}
	if len(s.phases) != 0 {
		t.Fatal("canceled observer attempted a new phase write")
	}
}

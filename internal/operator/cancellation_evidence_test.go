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

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/jobrun"
)

const cancellationPodProof = `{"source":"pods","job_uid":"original-job","pods":[{"uid":"original-pod","containers":[{"name":"task","state":{"terminated":{"finishedAt":"2026-10-29T00:00:00Z"}}}]}]}`

type cancellationJobs struct {
	*recoveryJobs
	beforeDelete func()
}

func (j *cancellationJobs) Delete(ctx context.Context, namespace, name string, uid ...string) error {
	if j.beforeDelete != nil {
		j.beforeDelete()
	}
	return j.fakeJobs.Delete(ctx, namespace, name, uid...)
}

type cancellationProofFailure struct {
	*recoveryRuns
	err error
}

func (s *cancellationProofFailure) RecordTerminationEvidence(context.Context, string, jobrun.Execution, []byte) error {
	return s.err
}

func cancellationFixture(t *testing.T) (Runtime, *recoveryRuns, *cancellationJobs, unstructured.Unstructured, v1alpha1.JobRunSpec) {
	t.Helper()
	r, s, original := recoveryFixture(t)
	s.stored.Phase = jobrun.CancelRequested
	s.e.PhaseUpdatedAt = r.now()
	s.e.Run = s.stored
	j := &cancellationJobs{recoveryJobs: original}
	r.Jobs = j
	obj, err := r.Dynamic.Resource(jobRunGVR).Namespace("finance").Get(t.Context(), v1alpha1.RunObjectName("nightly-report", "occ-1"), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := jobRunSpecFromUnstructured(*obj)
	if err != nil {
		t.Fatal(err)
	}
	return r, s, j, *obj, spec
}

func failedCancellationJob(j *cancellationJobs) {
	j.job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "DeadlineExceeded"}}
}

func TestLegacyCancellationPreservesAvailableProofBeforeDelete(t *testing.T) {
	r, s, j, obj, spec := cancellationFixture(t)
	failedCancellationJob(j)
	s.stored.AttemptPhase = jobrun.Failed
	j.proof, j.verified = []byte(cancellationPodProof), true
	j.beforeDelete = func() {
		if !jobrun.HasPodTerminationEvidence(s.stored.TerminationEvidence, s.stored.AttemptUID) {
			t.Fatal("existing legacy evidence deleted before commit")
		}
	}
	if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.Canceled || s.stored.AttemptPhase != jobrun.Failed || len(j.deleted) != 1 {
		t.Fatalf("incorrect legacy outcome: %+v", s.stored)
	}
}

func TestLegacyCancellationProofWriteFailureRetainsEvidence(t *testing.T) {
	r, s, j, obj, spec := cancellationFixture(t)
	failedCancellationJob(j)
	j.proof, j.verified = []byte(cancellationPodProof), true
	failure := errors.New("database transaction failed")
	r.Runs = &cancellationProofFailure{recoveryRuns: s, err: failure}
	if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); !errors.Is(err, failure) {
		t.Fatalf("failure=%v", err)
	}
	if len(j.deleted) != 0 || s.stored.Phase != jobrun.CancelRequested {
		t.Fatal("failed proof write deleted legacy evidence")
	}
}

func TestLegacyCancellationCannotManufactureJobPhaseProof(t *testing.T) {
	for _, savedTerminal := range []bool{false, true} {
		r, s, j, _, _ := cancellationFixture(t)
		failedCancellationJob(j)
		if savedTerminal {
			s.stored.AttemptPhase = jobrun.Failed
		}
		s.e.Run = s.stored
		if err := r.recoverExecution(t.Context(), executionTenant, s.e); err != nil {
			t.Fatal(err)
		}
		if s.proofWrites != 0 || s.stored.Phase != jobrun.CancelUnknown {
			t.Fatal("Job condition or saved attempt manufactured a stop fact")
		}
	}
}

func TestLegacyCancellationGapSurvivesLaterNarrowInventory(t *testing.T) {
	for _, mode := range []string{"unknown-since", "unknown-without-proof", "legacy-job-proof"} {
		t.Run(mode, func(t *testing.T) {
			r, s, j, obj, spec := cancellationFixture(t)
			failedCancellationJob(j)
			j.proof, j.verified = []byte(cancellationPodProof), true
			s.stored.Phase = jobrun.CancelUnknown
			if mode == "unknown-since" {
				at := r.now()
				s.stored.UnknownSince = &at
				s.stored.TerminationEvidence = []byte(cancellationPodProof)
			}
			if mode == "legacy-job-proof" {
				s.stored.TerminationEvidence = []byte(`{"source":"job","job_uid":"original-job"}`)
			}
			if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
				t.Fatal(err)
			}
			j.gone = true
			if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
				t.Fatal(err)
			}
			if s.stored.Phase != jobrun.CancelUnknown || s.proofWrites != 0 {
				t.Fatal("late legacy sample released uncertainty")
			}
		})
	}
}

func TestLegacyCancellationRetainedProofSurvivesDeletionTimeout(t *testing.T) {
	r, s, j, obj, spec := cancellationFixture(t)
	failedCancellationJob(j)
	j.proof, j.verified, j.deleteKeepsJob = []byte(cancellationPodProof), true, true
	s.e.PhaseUpdatedAt = r.now().Add(-6 * time.Minute)
	if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.CancelUnknown || !jobrun.HasPodTerminationEvidence(s.stored.TerminationEvidence, s.stored.AttemptUID) {
		t.Fatal("deletion delay discarded a durable stop")
	}
	j.gone, j.verified, j.proof = true, false, nil
	if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.Canceled {
		t.Fatal("retained legacy proof did not settle")
	}
}

func TestLegacyCancellationAmbiguityStopsProofAndDelete(t *testing.T) {
	for _, mode := range []string{"replacement", "owner", "NodeLost", "inventory-error"} {
		t.Run(mode, func(t *testing.T) {
			r, s, j, obj, spec := cancellationFixture(t)
			switch mode {
			case "replacement":
				j.job.UID = "replacement"
			case "owner":
				j.job.OwnerReferences = nil
			case "NodeLost":
				j.observationErr = execution.ErrPodIdentityAmbiguous
			case "inventory-error":
				j.observationErr = errors.New("incomplete list page")
			}
			err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec)
			if mode == "inventory-error" && err == nil {
				t.Fatal("failed observation suppressed")
			}
			if len(j.deleted) != 0 || s.proofWrites != 0 || s.stored.Phase == jobrun.Canceled {
				t.Fatal("unsafe observation authorized deletion or stop")
			}
		})
	}
}

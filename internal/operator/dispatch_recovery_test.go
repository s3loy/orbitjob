package operator

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/platform/scan"
)

func TestPossiblyDispatchedMissingJobNeverCreatesOnReconcileOrRecovery(t *testing.T) {
	r, s, j := recoveryFixture(t)
	s.stored.Phase = jobrun.CreatingAttempt
	s.stored.AttemptPhase = jobrun.CreatingAttempt
	s.stored.AttemptUID = ""
	s.e.Run = s.stored
	s.e.DispatchStage = jobrun.DispatchMayHaveStarted
	j.gone = true
	obj, err := r.Dynamic.Resource(jobRunGVR).Namespace(s.e.Namespace).Get(t.Context(), j.job.OwnerReferences[0].Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := r.ReconcileJobRun(t.Context(), *obj); err != nil {
			t.Fatal(err)
		}
		current, _, err := s.CurrentExecution(t.Context(), executionTenant, s.stored.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.recoverExecution(t.Context(), executionTenant, current); err != nil {
			t.Fatal(err)
		}
	}
	if s.stored.Phase != jobrun.ExecutionUnknown || s.stored.Attempt != 1 || s.stored.AttemptUID != "" {
		t.Fatalf("missing send identity escaped quarantine: %+v", s.stored)
	}
	if len(j.created) != 0 || len(s.attempts) != 0 || len(j.deleted) != 0 {
		t.Fatalf("observation created a replacement: jobs=%d attempts=%d deletes=%d", len(j.created), len(s.attempts), len(j.deleted))
	}
}

func TestUnboundCancellationKeepsLateOriginalDispatchObservable(t *testing.T) {
	r, s, j := recoveryFixture(t)
	s.stored.Phase = jobrun.CreatingAttempt
	s.stored.AttemptPhase = jobrun.CreatingAttempt
	s.stored.AttemptUID = ""
	s.e.DispatchStage = jobrun.DispatchMayHaveStarted
	j.gone = true
	obj, err := r.Dynamic.Resource(jobRunGVR).Namespace(s.e.Namespace).Get(t.Context(), j.job.OwnerReferences[0].Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// A recovered CR has a different UID. Cancellation must still find the Job
	// published by the original owner persisted before the send boundary.
	obj.SetUID(types.UID(scan.GenerateID()))
	spec, err := jobRunSpecFromUnstructured(*obj)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileCancellation(t.Context(), executionTenant, *obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.CancelUnknown || len(j.deleted) != 0 {
		t.Fatal("absence confirmed cancellation before delayed creation")
	}
	j.gone = false
	if err := r.reconcileCancellation(t.Context(), executionTenant, *obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.AttemptUID != string(j.job.UID) || s.stored.Phase == jobrun.Canceled || len(j.created) != 0 {
		t.Fatalf("late original dispatch was ignored or falsely stopped: %+v", s.stored)
	}
	j.gone = true
	if err := r.reconcileCancellation(t.Context(), executionTenant, *obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.CancelUnknown {
		t.Fatal("later absence fabricated a stop without evidence")
	}
}

type preparedCancellationRuns struct {
	*recoveryRuns
	canceled bool
}

func (s *preparedCancellationRuns) CancelPreparedAttempt(_ context.Context, _ string, _ jobrun.Execution) (bool, error) {
	if s.e.DispatchStage != jobrun.DispatchPrepared || s.stored.Phase != jobrun.CancelRequested {
		return false, errors.New("prepared cancellation boundary changed")
	}
	s.canceled = true
	s.stored.Phase = jobrun.Canceled
	s.stored.AttemptPhase = jobrun.Canceled
	return true, nil
}

func TestPreparedCancellationDoesNotNeedAnEmptyKubernetesInventory(t *testing.T) {
	r, original, j := recoveryFixture(t)
	original.stored.Phase = jobrun.CreatingAttempt
	original.stored.AttemptPhase = jobrun.CreatingAttempt
	original.stored.AttemptUID = ""
	original.e.DispatchStage = jobrun.DispatchPrepared
	s := &preparedCancellationRuns{recoveryRuns: original}
	r.Runs = s
	j.err = errors.New("no Kubernetes observation should be required")
	obj, err := r.Dynamic.Resource(jobRunGVR).Namespace(s.e.Namespace).Get(t.Context(), j.job.OwnerReferences[0].Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := jobRunSpecFromUnstructured(*obj)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileCancellation(t.Context(), executionTenant, *obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if !s.canceled || s.stored.Phase != jobrun.Canceled || len(j.created) != 0 || len(j.deleted) != 0 {
		t.Fatal("Prepared was not atomically canceled without external work")
	}
}

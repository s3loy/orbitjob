package operator

import (
	"context"
	"errors"
	"fmt"
	"slices"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/jobrun"
	corepostgres "orbitjob/internal/core/store/postgres"
)

type preparedAttemptCanceler interface {
	CancelPreparedAttempt(context.Context, string, jobrun.Execution) (bool, error)
}

// An unbound, possibly sent request may arrive after this observation. Only a
// locked Prepared record proves no send was authorized. Absence alone does not.
func (r Runtime) reconcileUnboundCancellation(ctx context.Context, tenant string, obj unstructured.Unstructured, stored jobrun.StoredRun, spec v1alpha1.JobRunSpec) error {
	r.requeueRun(obj)
	store := r.Runs.(executionRecoveryStore)
	e, found, err := store.CurrentExecution(ctx, tenant, stored.ID)
	if err != nil || !found {
		if err == nil {
			err = fmt.Errorf("current dispatch intent missing")
		}
		return err
	}
	if jobrun.Terminal(e.Run.Phase) {
		return r.patchRunStatus(ctx, obj, e.Run)
	}
	if e.Run.Phase != jobrun.CancelRequested && e.Run.Phase != jobrun.CancelUnknown {
		if _, err = r.Runs.UpdateRunPhase(ctx, tenant, e.Run.ID, jobrun.CancelRequested, e.Run.Attempt); err != nil {
			return err
		}
		e, found, err = store.CurrentExecution(ctx, tenant, stored.ID)
		if err != nil || !found {
			return err
		}
	}
	if e.Run.AttemptUID != "" {
		return r.reconcileBoundCancellation(ctx, tenant, obj, e.Run, spec)
	}
	if e.DispatchStage == jobrun.DispatchPrepared {
		if canceler, ok := r.Runs.(preparedAttemptCanceler); ok {
			changed, err := canceler.CancelPreparedAttempt(ctx, tenant, e)
			if errors.Is(err, corepostgres.ErrRunChanged) || errors.Is(err, jobrun.ErrRunTerminal) {
				return r.refreshRunStatus(ctx, tenant, obj, spec)
			}
			if err != nil {
				return err
			}
			if changed {
				if err = r.recordTerminal(ctx, tenant, e.Run.ID, jobrun.Canceled); err != nil {
					return err
				}
				e.Run.Phase = jobrun.Canceled
			}
			return r.patchRunStatus(ctx, obj, e.Run)
		}
	}
	job, exists, err := r.Jobs.Get(ctx, e.Namespace, e.Run.AttemptJobName)
	if err != nil {
		return r.cancellationObservationFailed(ctx, tenant, e, err)
	}
	if exists && e.Run.OwnerRunUID != "" {
		id, identityErr := execution.IdentityFromJob(job)
		if identityErr == nil && slices.Contains(v1alpha1.RunNameCandidates(e.DefinitionName, e.OccurrenceKey), id.RunName) &&
			execution.ValidateOwnership(job, execution.Identity{RunName: id.RunName, RunUID: e.Run.OwnerRunUID, RevisionID: fmt.Sprint(e.Run.RevisionID), Attempt: e.Run.Attempt}) == nil {
			if err = r.Runs.BindAttemptObservation(ctx, tenant, e.Run.ID, e.Run.Attempt, job.Name, string(job.UID), job.ResourceVersion); err != nil {
				return err
			}
			e, found, err = store.CurrentExecution(ctx, tenant, e.Run.ID)
			if err != nil || !found {
				return err
			}
			return r.reconcileBoundCancellation(ctx, tenant, obj, e.Run, spec)
		}
	}
	return r.markCancelUncertain(ctx, tenant, obj, e)
}

func (r Runtime) reconcileBoundCancellation(ctx context.Context, tenant string, obj unstructured.Unstructured, stored jobrun.StoredRun, spec v1alpha1.JobRunSpec) error {
	r.requeueRun(obj)
	store := r.Runs.(executionRecoveryStore)
	e, found, err := store.CurrentExecution(ctx, tenant, stored.ID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("current bound attempt missing")
	}
	if jobrun.Terminal(e.Run.Phase) {
		return r.patchRunStatus(ctx, obj, e.Run)
	}
	if e.Run.Phase != jobrun.CancelRequested && e.Run.Phase != jobrun.CancelUnknown {
		if _, err = r.Runs.UpdateRunPhase(ctx, tenant, e.Run.ID, jobrun.CancelRequested, e.Run.Attempt); err != nil {
			return err
		}
		e, found, err = store.CurrentExecution(ctx, tenant, stored.ID)
		if err != nil || !found {
			return err
		}
		if jobrun.Terminal(e.Run.Phase) {
			return r.patchRunStatus(ctx, obj, e.Run)
		}
	}
	job, exists, err := r.Jobs.Get(ctx, e.Namespace, e.Run.AttemptJobName)
	if err != nil {
		return r.cancellationObservationFailed(ctx, tenant, e, err)
	}
	retained := execution.RetainsTerminationEvidence(job) || jobrun.UsesPodEvidenceRetention(e.Run.TerminationEvidence)
	if exists && !ownsCancellationJob(job, e) {
		executionObservations.WithLabelValues("cancel_identity_conflict").Inc()
		return r.invalidateCancelProof(ctx, tenant, obj, e, retained)
	}
	if retained {
		if err = r.settleRetainedCancellation(ctx, tenant, e, job); err != nil {
			return r.cancellationObservationFailed(ctx, tenant, e, err)
		}
	} else if err = r.observeLegacyCancellation(ctx, tenant, e, job); err != nil {
		return r.cancellationObservationFailed(ctx, tenant, e, err)
	}
	e, found, err = store.CurrentExecution(ctx, tenant, e.Run.ID)
	if err != nil || !found {
		return err
	}
	if jobrun.Terminal(e.Run.Phase) {
		return r.patchRunStatus(ctx, obj, e.Run)
	}
	trusted := e.Run.UnknownSince == nil && jobrun.HasPodTerminationEvidence(e.Run.TerminationEvidence, e.Run.AttemptUID)
	if retained {
		trusted = trusted && jobrun.HasRetainedTerminationEvidence(e.Run.TerminationEvidence, e.Run.AttemptUID) && jobrun.EvidenceReleaseComplete(e.Run.TerminationEvidence)
	}
	after, stillExists, err := r.Jobs.Get(ctx, e.Namespace, e.Run.AttemptJobName)
	if err != nil {
		return r.cancellationObservationFailed(ctx, tenant, e, err)
	}
	if stillExists && !ownsCancellationJob(after, e) {
		return r.invalidateCancelProof(ctx, tenant, obj, e, retained)
	}
	if !trusted || stillExists {
		if !stillExists && !trusted {
			return r.invalidateCancelProof(ctx, tenant, obj, e, retained)
		}
		return r.waitCancellation(ctx, tenant, obj, e)
	}
	// Keep an independently recorded attempt failure while the logical run
	// records cancellation. The transaction rechecks proof and isolation.
	if !jobrun.Terminal(e.Run.AttemptPhase) && e.Run.AttemptPhase != jobrun.RetryWaiting {
		id, number, observeErr := r.Runs.UpdateAttemptPhase(ctx, tenant, e.Run.AttemptJobName, string(jobrun.Canceled), "", "")
		if observeErr != nil && !errors.Is(observeErr, corepostgres.ErrStaleObservation) {
			return observeErr
		}
		if observeErr == nil && (id != e.Run.ID || number != e.Run.Attempt) {
			return corepostgres.ErrRunChanged
		}
	}
	changed, err := r.Runs.UpdateRunPhase(ctx, tenant, e.Run.ID, jobrun.Canceled, e.Run.Attempt)
	if err != nil {
		return err
	}
	if changed {
		if err = r.recordTerminal(ctx, tenant, e.Run.ID, jobrun.Canceled); err != nil {
			return err
		}
	}
	return r.refreshRunStatus(ctx, tenant, obj, spec)
}

// An unavailable observer does not create an evidence gap, but it cannot extend
// the committed confirmation budget. Preserve its error after recording timeout.
func (r Runtime) cancellationObservationFailed(ctx context.Context, tenant string, e jobrun.Execution, observationErr error) error {
	if err := ctx.Err(); err != nil {
		return errors.Join(observationErr, err)
	}
	if e.Run.Phase == jobrun.CancelUnknown || (!e.PhaseUpdatedAt.IsZero() && r.now().Sub(e.PhaseUpdatedAt) < cancelConfirmTimeout) {
		return observationErr
	}
	_, phaseErr := r.Runs.UpdateRunPhase(ctx, tenant, e.Run.ID, jobrun.CancelUnknown, e.Run.Attempt)
	if phaseErr == nil {
		executionObservations.WithLabelValues("cancel_confirmation_timeout").Inc()
	}
	return errors.Join(observationErr, phaseErr)
}

func (r Runtime) settleRetainedCancellation(ctx context.Context, tenant string, e jobrun.Execution, job *batchv1.Job) error {
	store, ok := r.Runs.(retainedEvidenceStore)
	if !ok {
		return fmt.Errorf("retained Pod evidence store required")
	}
	a, found, err := store.RecordedExecution(ctx, tenant, e.Namespace, e.Run.AttemptJobName, e.Run.AttemptUID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("retained cancellation attempt missing")
	}
	if jobrun.UsesPodEvidenceRetention(a.TerminationEvidence) && !jobrun.HasRetainedTerminationEvidence(a.TerminationEvidence, a.JobUID) {
		_, err = r.settlePodEvidence(ctx, tenant, a, job)
		return err
	}
	if job != nil && execution.StopBoundary(job) == "" && job.DeletionTimestamp == nil {
		stopper, ok := r.Jobs.(interface {
			RequestStop(context.Context, *batchv1.Job) (bool, error)
		})
		if !ok {
			return fmt.Errorf("guarded Job stop request adapter required")
		}
		changed, stopErr := stopper.RequestStop(ctx, job)
		if stopErr != nil {
			executionObservations.WithLabelValues("cancel_stop_error").Inc()
			return fmt.Errorf("request cancellation deadline for run %d: %w", e.Run.ID, stopErr)
		}
		if changed {
			executionObservations.WithLabelValues("cancel_stop_requested").Inc()
		}
		job, _, err = r.Jobs.Get(ctx, e.Namespace, a.JobName)
		if err != nil {
			return err
		}
	}
	settled, err := r.settlePodEvidence(ctx, tenant, a, job)
	if err != nil || !settled {
		return err
	}
	if job != nil && job.DeletionTimestamp == nil {
		if err = r.Jobs.Delete(ctx, e.Namespace, a.JobName, a.JobUID); err != nil {
			return fmt.Errorf("delete proven stopped Job for run %d: %w", e.Run.ID, err)
		}
		executionObservations.WithLabelValues("cancel_cleanup_requested").Inc()
	}
	return nil
}

// Legacy Jobs cannot reconstruct inventory already lost before this upgrade.
// Preserve a terminal Pod proof before deletion when it is still available;
// otherwise request stop and retain explicit uncertainty without a new attempt.
func (r Runtime) observeLegacyCancellation(ctx context.Context, tenant string, e jobrun.Execution, job *batchv1.Job) error {
	store := r.Runs.(executionRecoveryStore)
	observer, ok := r.Jobs.(interface {
		InspectTermination(context.Context, string, string, string) ([]byte, bool, error)
	})
	if !ok {
		return fmt.Errorf("UID-based cancellation Pod observer required")
	}
	proof, verified, err := observer.InspectTermination(ctx, e.Namespace, e.Run.AttemptJobName, e.Run.AttemptUID)
	if errors.Is(err, execution.ErrPodIdentityAmbiguous) || (errors.Is(err, execution.ErrPodWorkloadActive) && jobrun.HasPodTerminationEvidence(e.Run.TerminationEvidence, e.Run.AttemptUID)) {
		return store.InvalidatePodTerminationEvidence(ctx, tenant, e)
	}
	if err != nil && !errors.Is(err, execution.ErrPodWorkloadActive) {
		return err
	}
	terminal := job != nil && (execution.Phase(*job) == execution.PhaseSucceeded || execution.Phase(*job) == execution.PhaseFailed)
	if terminal && verified && e.Run.UnknownSince == nil &&
		(e.Run.Phase != jobrun.CancelUnknown || jobrun.HasPodTerminationEvidence(e.Run.TerminationEvidence, e.Run.AttemptUID)) {
		if err = store.RecordTerminationEvidence(ctx, tenant, e, proof); err != nil {
			return err
		}
	}
	if job != nil && job.DeletionTimestamp == nil && e.Run.UnknownSince == nil {
		return r.Jobs.Delete(ctx, e.Namespace, e.Run.AttemptJobName, e.Run.AttemptUID)
	}
	return nil
}

func ownsCancellationJob(job *batchv1.Job, e jobrun.Execution) bool {
	if job == nil || string(job.UID) != e.Run.AttemptUID {
		return false
	}
	id, err := execution.IdentityFromJob(job)
	if err != nil || id.RunUID == "" || !slices.Contains(v1alpha1.RunNameCandidates(e.DefinitionName, e.OccurrenceKey), id.RunName) ||
		(e.Run.OwnerRunUID != "" && id.RunUID != e.Run.OwnerRunUID) ||
		execution.ValidateOwnership(job, execution.Identity{RunName: id.RunName, RunUID: id.RunUID, RevisionID: fmt.Sprint(e.Run.RevisionID), Attempt: e.Run.Attempt}) != nil {
		return false
	}
	for _, owner := range job.OwnerReferences {
		if owner.Controller != nil && *owner.Controller && owner.Kind == "JobRun" && owner.APIVersion == v1alpha1.GroupVersion.String() && string(owner.UID) == id.RunUID && owner.Name == id.RunName {
			return true
		}
	}
	return false
}

func (r Runtime) waitCancellation(ctx context.Context, tenant string, obj unstructured.Unstructured, e jobrun.Execution) error {
	if e.Run.UnknownSince != nil || e.Run.Phase == jobrun.CancelUnknown || e.PhaseUpdatedAt.IsZero() || r.now().Sub(e.PhaseUpdatedAt) >= cancelConfirmTimeout {
		executionObservations.WithLabelValues("cancel_confirmation_timeout").Inc()
		return r.markCancelUncertain(ctx, tenant, obj, e)
	}
	return r.patchRunStatus(ctx, obj, e.Run)
}

func (r Runtime) invalidateCancelProof(ctx context.Context, tenant string, obj unstructured.Unstructured, e jobrun.Execution, retained bool) error {
	if retained {
		store, ok := r.Runs.(retainedEvidenceStore)
		if !ok {
			return fmt.Errorf("retained Pod evidence store required")
		}
		a, found, err := store.RecordedExecution(ctx, tenant, e.Namespace, e.Run.AttemptJobName, e.Run.AttemptUID)
		if err != nil {
			return err
		}
		if found {
			if err = r.isolatePodEvidence(ctx, tenant, a, jobrun.ReasonPodIdentityAmbiguous); err != nil {
				return err
			}
		}
	} else if err := r.Runs.(executionRecoveryStore).InvalidatePodTerminationEvidence(ctx, tenant, e); err != nil {
		return err
	}
	return r.markCancelUncertain(ctx, tenant, obj, e)
}

func (r Runtime) markCancelUncertain(ctx context.Context, tenant string, obj unstructured.Unstructured, e jobrun.Execution) error {
	if e.Run.Phase != jobrun.CancelUnknown {
		if _, err := r.Runs.UpdateRunPhase(ctx, tenant, e.Run.ID, jobrun.CancelUnknown, e.Run.Attempt); err != nil {
			return err
		}
	}
	e.Run.Phase = jobrun.CancelUnknown
	return r.patchRunStatus(ctx, obj, e.Run)
}

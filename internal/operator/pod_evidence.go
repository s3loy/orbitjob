package operator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/jobrun"
	corepostgres "orbitjob/internal/core/store/postgres"
)

type retainedEvidenceStore interface {
	RecordedExecution(context.Context, string, string, string, string) (jobrun.RecordedAttempt, bool, error)
	PendingEvidenceAfter(context.Context, string, int64, int) ([]jobrun.RecordedAttempt, error)
	RecordAttemptTerminationEvidence(context.Context, string, jobrun.RecordedAttempt, []byte) error
	MarkEvidenceReleased(context.Context, string, jobrun.RecordedAttempt) error
	IsolateAttemptEvidence(context.Context, string, jobrun.RecordedAttempt, string) error
}

type retainedEvidenceObserver interface {
	InspectRetainedTermination(context.Context, *batchv1.Job, []byte) ([]byte, bool, error)
	ReleaseTerminationEvidence(context.Context, string, string, string, []byte) error
}

var _ retainedEvidenceStore = (*corepostgres.ControlPlaneRepository)(nil)
var _ retainedEvidenceObserver = execution.Adapter{}

func ownsRecordedJob(job *batchv1.Job, a jobrun.RecordedAttempt) bool {
	if job == nil || string(job.UID) != a.JobUID || job.Name != a.JobName || job.Namespace != a.Execution.Namespace {
		return false
	}
	id, err := execution.IdentityFromJob(job)
	if err != nil || id.RunUID == "" || a.OwnerRunUID != id.RunUID ||
		!slices.Contains(v1alpha1.RunNameCandidates(a.Execution.DefinitionName, a.Execution.OccurrenceKey), id.RunName) {
		return false
	}
	owner := metav1.GetControllerOf(job)
	if owner == nil || owner.APIVersion != v1alpha1.GroupVersion.String() || owner.Kind != "JobRun" ||
		owner.Name != id.RunName || string(owner.UID) != a.OwnerRunUID {
		return false
	}
	return execution.ValidateOwnership(job, execution.Identity{RunName: id.RunName, RunUID: a.OwnerRunUID,
		RevisionID: fmt.Sprint(a.Execution.Run.RevisionID), Attempt: a.Number}) == nil
}

// settlePodEvidence is shared by ordinary Job observation, cancellation and the
// bounded restart lane. It never advances a run or substitutes a historical
// attempt number for the current run identity.
func (r Runtime) settlePodEvidence(ctx context.Context, tenant string, a jobrun.RecordedAttempt, job *batchv1.Job) (bool, error) {
	retained := execution.RetainsTerminationEvidence(job) || jobrun.UsesPodEvidenceRetention(a.TerminationEvidence)
	if !retained {
		return true, nil
	}
	store, ok := r.Runs.(retainedEvidenceStore)
	if !ok {
		return false, fmt.Errorf("retained Pod evidence store required")
	}
	observer, ok := r.Jobs.(retainedEvidenceObserver)
	if !ok {
		return false, fmt.Errorf("retained Pod evidence observer required")
	}
	if a.UnknownSince != nil {
		executionObservations.WithLabelValues("hold_isolated").Inc()
		return false, nil
	}
	if jobrun.UsesPodEvidenceRetention(a.TerminationEvidence) && !jobrun.HasRetainedTerminationEvidence(a.TerminationEvidence, a.JobUID) {
		// Older deletion-only checkpoints remain cleanup obligations, including
		// those marked done. They cannot be recast as acknowledged inventories.
		return false, r.isolatePodEvidence(ctx, tenant, a, jobrun.ReasonPodIdentityAmbiguous)
	}
	if job != nil && !ownsRecordedJob(job, a) {
		return false, r.isolatePodEvidence(ctx, tenant, a, jobrun.ReasonJobUIDMismatch)
	}
	proof := a.TerminationEvidence
	trusted := jobrun.HasRetainedTerminationEvidence(proof, a.JobUID)
	if job != nil {
		observed, complete, err := observer.InspectRetainedTermination(ctx, job, proof)
		if errors.Is(err, execution.ErrPodIdentityAmbiguous) || (trusted && errors.Is(err, execution.ErrPodWorkloadActive)) {
			return false, r.isolatePodEvidence(ctx, tenant, a, jobrun.ReasonPodIdentityAmbiguous)
		}
		if errors.Is(err, execution.ErrPodWorkloadActive) || (err == nil && !complete) {
			executionObservations.WithLabelValues("hold_waiting").Inc()
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("inspect held Pods for run %d attempt %d: %w", a.Execution.Run.ID, a.Number, err)
		}
		if !trusted {
			if err = store.RecordAttemptTerminationEvidence(ctx, tenant, a, observed); err != nil {
				executionObservations.WithLabelValues("hold_proof_write_error").Inc()
				return false, err
			}
			executionObservations.WithLabelValues("hold_proof_committed").Inc()
		}
	} else if !trusted {
		return false, r.isolatePodEvidence(ctx, tenant, a, jobrun.ReasonJobNotFound)
	}
	// Always read the committed proof, including a concurrent isolation, before
	// crossing the database-to-Kubernetes cleanup boundary.
	saved, found, err := store.RecordedExecution(ctx, tenant, a.Execution.Namespace, a.JobName, a.JobUID)
	if err != nil {
		return false, err
	}
	if !found || saved.ID != a.ID || saved.UnknownSince != nil || !jobrun.HasRetainedTerminationEvidence(saved.TerminationEvidence, a.JobUID) {
		return false, nil
	}
	if err = observer.ReleaseTerminationEvidence(ctx, a.Execution.Namespace, a.JobName, a.JobUID, saved.TerminationEvidence); err != nil {
		if errors.Is(err, execution.ErrPodIdentityAmbiguous) || errors.Is(err, execution.ErrPodWorkloadActive) {
			return false, r.isolatePodEvidence(ctx, tenant, saved, jobrun.ReasonPodIdentityAmbiguous)
		}
		executionObservations.WithLabelValues("hold_release_error").Inc()
		return false, fmt.Errorf("release held Pods for run %d attempt %d: %w", a.Execution.Run.ID, a.Number, err)
	}
	if !jobrun.EvidenceReleaseComplete(saved.TerminationEvidence) {
		if err = store.MarkEvidenceReleased(ctx, tenant, saved); err != nil {
			return false, err
		}
		executionObservations.WithLabelValues("hold_released").Inc()
	}
	return true, nil
}

func (r Runtime) isolatePodEvidence(ctx context.Context, tenant string, a jobrun.RecordedAttempt, reason string) error {
	if err := r.Runs.(retainedEvidenceStore).IsolateAttemptEvidence(ctx, tenant, a, reason); err != nil {
		return err
	}
	executionObservations.WithLabelValues("hold_evidence_gap").Inc()
	slog.WarnContext(ctx, "execution Pod evidence isolated", "tenant", tenant, "run_id", a.Execution.Run.ID, "attempt", a.Number, "reason", reason)
	return nil
}

func (r Runtime) settleObservedJob(ctx context.Context, tenant string, job *batchv1.Job, identity execution.Identity) (bool, error) {
	if !execution.RetainsTerminationEvidence(job) {
		return true, nil
	}
	store, ok := r.Runs.(retainedEvidenceStore)
	if !ok {
		return false, fmt.Errorf("retained Pod evidence store required")
	}
	a, found, err := store.RecordedExecution(ctx, tenant, job.Namespace, job.Name, string(job.UID))
	if err != nil {
		return false, err
	}
	if (!found || a.JobUID == "") && r.Dynamic != nil {
		if err = r.bindObservedAttempt(ctx, tenant, job, identity); err != nil {
			if errors.Is(err, corepostgres.ErrNoAttempt) || errors.Is(err, corepostgres.ErrStaleObservation) {
				return false, nil
			}
			return false, err
		}
		a, found, err = store.RecordedExecution(ctx, tenant, job.Namespace, job.Name, string(job.UID))
		if err != nil {
			return false, err
		}
	}
	if !found {
		return false, fmt.Errorf("retained Job has no recorded attempt: %s/%s", job.Namespace, job.Name)
	}
	// Nonterminal observation remains useful for run visibility. Only a stop
	// boundary or outstanding proof needs settlement before phase progression.
	if execution.StopBoundary(job) == "" && !jobrun.EvidenceReleasePending(a.TerminationEvidence) {
		return true, nil
	}
	return r.settlePodEvidence(ctx, tenant, a, job)
}

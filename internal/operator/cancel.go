package operator

import (
	"context"
	"errors"
	"fmt"
	corepostgres "orbitjob/internal/core/store/postgres"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/jobrun"
)

// cancelConfirmTimeout bounds confirmation from the persisted cancel request,
// including workload termination and deletion. Reporting CancelUnknown
// is the honest outcome: the container may still be running.
const cancelConfirmTimeout = 5 * time.Minute

var errUnrecordedExecutionUnknown = errors.New("unrecorded legacy execution remains uncertain")

// reconcileCancellation drives a stop request to a confirmed state.
//
// A cancel is never recorded as done from the request alone. The sequence is
// intent -> stop the bound Job -> persist Pod exits -> delete -> Canceled. If the Job
// does not disappear inside the timeout the run lands in CancelUnknown, which
// tells operators that external side effects may still be occurring instead of
// claiming a clean stop the platform cannot prove.
func (r Runtime) reconcileCancellation(
	ctx context.Context, tenant string, obj unstructured.Unstructured,
	stored jobrun.StoredRun, spec v1alpha1.JobRunSpec,
) error {
	if _, ok := r.Runs.(executionRecoveryStore); ok && stored.Attempt > 0 {
		if stored.AttemptUID == "" {
			return r.reconcileUnboundCancellation(ctx, tenant, obj, stored, spec)
		}
		return r.reconcileBoundCancellation(ctx, tenant, obj, stored, spec)
	}
	if r.EnqueueAfter != nil {
		defer r.EnqueueAfter("jobruns", obj.GetNamespace(), obj.GetName(), 5*time.Second)
	}
	// Nothing was ever started, so there is no external side effect to stop and
	// the cancel is already a fact. Recording an intermediate request here would
	// only leave a state a reader could mistake for work in progress.
	if stored.Attempt == 0 {
		if _, ok := r.Runs.(executionRecoveryStore); ok && stored.Phase == jobrun.CancelUnknown {
			err := r.stopUnrecordedAttempt(ctx, tenant, obj, stored)
			if err != nil && !errors.Is(err, errUnrecordedExecutionUnknown) && !errors.Is(err, corepostgres.ErrAttemptOwnership) {
				return err
			}
			return r.refreshRunStatus(ctx, tenant, obj, spec)
		}
		if err := r.stopUnrecordedAttempt(ctx, tenant, obj, stored); err != nil {
			if errors.Is(err, errUnrecordedExecutionUnknown) {
				return r.refreshRunStatus(ctx, tenant, obj, spec)
			}
			return err
		}
		changed, err := r.Runs.UpdateRunPhase(ctx, tenant, stored.ID, jobrun.Canceled, 0)
		if err != nil {
			return err
		}
		if !changed {
			return r.refreshRunStatus(ctx, tenant, obj, spec)
		}
		if changed {
			if err := r.recordTerminal(ctx, tenant, stored.ID, jobrun.Canceled); err != nil {
				return err
			}
		}
		stored.Phase = jobrun.Canceled
		return r.patchRunStatus(ctx, obj, stored)
	}

	if stored.Phase != jobrun.CancelRequested && stored.Phase != jobrun.CancelUnknown {
		requested, err := jobrun.RequestCancel(jobrun.JobRun{Phase: stored.Phase}, r.now())
		if err != nil {
			return fmt.Errorf("request cancel for run %d: %w", stored.ID, err)
		}
		changed, err := r.Runs.UpdateRunPhase(ctx, tenant, stored.ID, requested.Phase, stored.Attempt)
		if err != nil {
			return err
		}
		if !changed {
			return r.refreshRunStatus(ctx, tenant, obj, spec)
		}
		stored.Phase = requested.Phase
	}

	jobName := stored.AttemptJobName
	if jobName == "" {
		jobName = execution.JobName(execution.Identity{RunName: obj.GetName(), Attempt: stored.Attempt})
	}
	before, exists, err := r.Jobs.Get(ctx, obj.GetNamespace(), jobName)
	if err != nil {
		return err
	}
	uid := ""
	if exists {
		if err := execution.ValidateOwnership(before, execution.Identity{RunName: obj.GetName(), RunUID: string(obj.GetUID()), Attempt: stored.Attempt}); err != nil {
			return err
		}
		uid = string(before.UID)
		if stored.AttemptUID == "" {
			if err := r.Runs.BindAttemptObservation(ctx, tenant, stored.ID, stored.Attempt, jobName, uid, before.ResourceVersion); err != nil {
				return err
			}
		}
	}
	if store, ok := r.Runs.(executionRecoveryStore); ok {
		e, found, err := store.CurrentExecution(ctx, tenant, stored.ID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("current cancellation intent missing")
		}
		if e.Run.AttemptUID != "" {
			return r.reconcileBoundCancellation(ctx, tenant, obj, e.Run, spec)
		}
		return r.markCancelUncertain(ctx, tenant, obj, e)
	}
	if err := r.Jobs.Delete(ctx, obj.GetNamespace(), jobName, uid); err != nil {
		return fmt.Errorf("delete kubernetes job %s: %w", jobName, err)
	}

	job, found, err := r.Jobs.Get(ctx, obj.GetNamespace(), jobName)
	if err != nil {
		return fmt.Errorf("observe kubernetes job %s: %w", jobName, err)
	}
	if found && job.DeletionTimestamp == nil {
		// The delete has not been registered yet; the next reconcile retries.
		return r.patchRunStatus(ctx, obj, stored)
	}
	if found && r.now().Sub(job.DeletionTimestamp.Time) > cancelConfirmTimeout {
		if _, err := r.Runs.UpdateRunPhase(ctx, tenant, stored.ID, jobrun.CancelUnknown, stored.Attempt); err != nil {
			return err
		}
		stored.Phase = jobrun.CancelUnknown
		return r.patchRunStatus(ctx, obj, stored)
	}
	if found {
		// Deletion is in flight; wait for it rather than guessing.
		return r.patchRunStatus(ctx, obj, stored)
	}

	stopped, err := r.Jobs.Stopped(ctx, obj.GetNamespace(), jobName)
	if err != nil {
		return err
	}
	if !stopped {
		if stored.Phase != jobrun.CancelUnknown {
			if _, err := r.Runs.UpdateRunPhase(ctx, tenant, stored.ID, jobrun.CancelUnknown, stored.Attempt); err != nil {
				return err
			}
		}
		stored.Phase = jobrun.CancelUnknown
		return r.patchRunStatus(ctx, obj, stored)
	}

	if stored.AttemptUID == "" && !exists {
		if stored.Phase != jobrun.CancelUnknown {
			if _, err := r.Runs.UpdateRunPhase(ctx, tenant, stored.ID, jobrun.CancelUnknown, stored.Attempt); err != nil {
				return err
			}
		}
		stored.Phase = jobrun.CancelUnknown
		return r.patchRunStatus(ctx, obj, stored)
	}

	if _, _, err := r.Runs.UpdateAttemptPhase(ctx, tenant, jobName, string(jobrun.Canceled), "", ""); err != nil && !errors.Is(err, corepostgres.ErrStaleObservation) {
		return fmt.Errorf("record canceled attempt %s: %w", jobName, err)
	}
	changed, err := r.Runs.UpdateRunPhase(ctx, tenant, stored.ID, jobrun.Canceled, stored.Attempt)
	if err != nil {
		return err
	}
	if !changed {
		return r.refreshRunStatus(ctx, tenant, obj, spec)
	}
	if changed {
		if err := r.recordTerminal(ctx, tenant, stored.ID, jobrun.Canceled); err != nil {
			return err
		}
	}
	stored.Phase = jobrun.Canceled
	return r.patchRunStatus(ctx, obj, stored)
}

func (r Runtime) refreshRunStatus(ctx context.Context, tenant string, obj unstructured.Unstructured, spec v1alpha1.JobRunSpec) error {
	latest, found, err := r.Runs.RunByOccurrence(ctx, tenant, spec.ScheduledJobRef.UID, spec.OccurrenceKey)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return r.patchRunStatus(ctx, obj, latest)
}

// An old process may have created a deterministic Job before it recorded the
// attempt. New writers reserve first; upgrade recovery also stops owned Jobs
// left in the former crash window before acknowledging an attempt-zero stop.
func (r Runtime) stopUnrecordedAttempt(ctx context.Context, tenant string, obj unstructured.Unstructured, stored jobrun.StoredRun) error {
	if r.Jobs == nil {
		return fmt.Errorf("job manager required for cancellation confirmation")
	}
	id := execution.Identity{RunName: obj.GetName(), RunUID: string(obj.GetUID()), RevisionID: fmt.Sprint(stored.RevisionID), Attempt: 1}
	for _, name := range execution.JobNameCandidates(id) {
		job, found, err := r.Jobs.Get(ctx, obj.GetNamespace(), name)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if err := execution.ValidateOwnership(job, id); err != nil {
			if _, strict := r.Runs.(executionRecoveryStore); !strict {
				return err
			}
			marker, ok := r.Runs.(interface {
				MarkConflictingUnrecordedExecutionUnknown(context.Context, string, int64, string) error
			})
			if !ok {
				return fmt.Errorf("durable legacy identity conflict isolation is required: %w", err)
			}
			if err := marker.MarkConflictingUnrecordedExecutionUnknown(ctx, tenant, stored.ID, string(job.UID)); err != nil {
				return err
			}
			return errUnrecordedExecutionUnknown
		}
		if job.UID == "" {
			return fmt.Errorf("owned workload has no verifiable uid")
		}
		if _, strict := r.Runs.(executionRecoveryStore); strict {
			marker, ok := r.Runs.(interface {
				MarkUnrecordedExecutionUnknown(context.Context, string, int64, string) error
			})
			if !ok {
				return fmt.Errorf("durable legacy execution isolation is required")
			}
			// Commit the evidence gap before DELETE. A restart must never turn
			// the ensuing empty inventory into a fabricated no-execution fact.
			if err := marker.MarkUnrecordedExecutionUnknown(ctx, tenant, stored.ID, string(job.UID)); err != nil {
				return err
			}
			if err := r.Jobs.Delete(ctx, obj.GetNamespace(), name, string(job.UID)); err != nil {
				return err
			}
			return errUnrecordedExecutionUnknown
		}
		if err := r.Jobs.Delete(ctx, obj.GetNamespace(), name, string(job.UID)); err != nil {
			return err
		}
		stopped, err := r.Jobs.Stopped(ctx, obj.GetNamespace(), name)
		if err != nil {
			return err
		}
		if !stopped {
			return fmt.Errorf("unrecorded workload %s is still stopping", name)
		}
	}
	return nil
}

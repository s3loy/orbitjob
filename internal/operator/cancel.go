package operator

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/jobrun"
)

// cancelConfirmTimeout bounds how long the platform waits for a deleted Job to
// disappear before admitting it cannot confirm the stop. Reporting CancelUnknown
// is the honest outcome: the container may still be running.
const cancelConfirmTimeout = 5 * time.Minute

// reconcileCancellation drives a stop request to a confirmed state.
//
// A cancel is never recorded as done from the request alone. The sequence is
// intent -> delete the Kubernetes Job -> observe it gone -> Canceled. If the Job
// does not disappear inside the timeout the run lands in CancelUnknown, which
// tells operators that external side effects may still be occurring instead of
// claiming a clean stop the platform cannot prove.
func (r Runtime) reconcileCancellation(
	ctx context.Context, tenant string, obj unstructured.Unstructured,
	stored jobrun.StoredRun, spec v1alpha1.JobRunSpec,
) error {
	// Nothing was ever started, so there is no external side effect to stop and
	// the cancel is already a fact. Recording an intermediate request here would
	// only leave a state a reader could mistake for work in progress.
	if stored.Attempt == 0 {
		changed, err := r.Runs.UpdateRunPhase(ctx, tenant, stored.ID, jobrun.Canceled, 0)
		if err != nil {
			return err
		}
		r.recordFunctionOutcome(ctx, tenant, stored.ID, jobrun.Canceled, changed)
		stored.Phase = jobrun.Canceled
		return r.patchRunStatus(ctx, obj, stored)
	}

	if stored.Phase != jobrun.CancelRequested {
		requested, err := jobrun.RequestCancel(jobrun.JobRun{Phase: stored.Phase}, r.now())
		if err != nil {
			return fmt.Errorf("request cancel for run %d: %w", stored.ID, err)
		}
		if _, err := r.Runs.UpdateRunPhase(ctx, tenant, stored.ID, requested.Phase, stored.Attempt); err != nil {
			return err
		}
		stored.Phase = requested.Phase
	}

	jobName := execution.JobName(execution.Identity{
		RunName: obj.GetName(), Attempt: stored.Attempt,
	})
	if err := r.Jobs.Delete(ctx, obj.GetNamespace(), jobName); err != nil {
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

	if _, _, err := r.Runs.UpdateAttemptPhase(ctx, tenant, jobName, string(jobrun.Canceled), "", ""); err != nil {
		return fmt.Errorf("record canceled attempt %s: %w", jobName, err)
	}
	changed, err := r.Runs.UpdateRunPhase(ctx, tenant, stored.ID, jobrun.Canceled, stored.Attempt)
	if err != nil {
		return err
	}
	r.recordFunctionOutcome(ctx, tenant, stored.ID, jobrun.Canceled, changed)
	stored.Phase = jobrun.Canceled
	return r.patchRunStatus(ctx, obj, stored)
}

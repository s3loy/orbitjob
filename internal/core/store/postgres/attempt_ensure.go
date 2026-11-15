package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"orbitjob/internal/core/domain/jobrun"
)

const lockAttemptDispatchSQL = `SELECT r.phase,r.attempt,r.actor,COALESCE(a.kubernetes_job_uid,''),a.id,a.dispatch_stage
FROM job_run_control_plane r JOIN job_run_attempts_control_plane a ON a.run_id=r.id AND a.attempt_number=$3 AND a.tenant_id=r.tenant_id
WHERE r.tenant_id=$1 AND r.id=$2 FOR UPDATE OF r,a`

type lockedAttemptDispatch struct {
	phase      jobrun.Phase
	number     int
	actor, uid string
	id         int64
	stage      jobrun.DispatchStage
}

func readAttemptDispatch(ctx context.Context, tx *sql.Tx, tenant string, runID int64, number int) (lockedAttemptDispatch, error) {
	keys, _, err := lockRunExecutionSources(ctx, tx, tenant, runID)
	if err != nil {
		return lockedAttemptDispatch{}, err
	}
	if err := lockWorkflowParentForAttempt(ctx, tx, tenant, runID); err != nil {
		return lockedAttemptDispatch{}, err
	}
	var d lockedAttemptDispatch
	if err := tx.QueryRowContext(ctx, lockAttemptDispatchSQL, tenant, runID, number).Scan(&d.phase, &d.number, &d.actor, &d.uid, &d.id, &d.stage); err != nil {
		return d, err
	}
	if jobrun.Terminal(d.phase) {
		return d, jobrun.ErrRunTerminal
	}
	if d.number != number || (d.phase != jobrun.CreatingAttempt && d.phase != jobrun.Running) {
		return d, ErrRunChanged
	}
	if d.stage == jobrun.DispatchPrepared {
		if err := checkExecutionSources(ctx, tx, tenant, keys); err != nil {
			return d, err
		}
	}
	return d, nil
}

// CreateAttemptWithEnsure commits Prepared intent, then irrevocably marks the
// request as possibly sent before entering the external callback. Only this
// invocation's successful marker commit permits one callback. A lost commit
// response or any callback error leaves recovery observation-only.
func (r *ControlPlaneRepository) CreateAttemptWithEnsure(ctx context.Context, tenantID string, runID int64, attempt jobrun.Attempt, ensure func(context.Context) (jobrun.Attempt, error)) error {
	if ensure == nil {
		return fmt.Errorf("attempt ensure callback is required")
	}
	intent := attempt
	intent.Phase = jobrun.CreatingAttempt
	intent.KubernetesJobUID = ""
	if err := r.createAttemptForTenant(ctx, tenantID, runID, intent, jobrun.CreatingAttempt, true); err != nil {
		return err
	}
	// The budget includes marker locking, the external call and final binding.
	lockCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	claimed := false
	if err := r.WithTenantTransaction(lockCtx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		d, err := readAttemptDispatch(ctx, tx, tenantID, runID, attempt.Number)
		if err != nil {
			return err
		}
		if d.uid != "" {
			if d.phase == jobrun.Running {
				return nil
			}
			if _, err = tx.ExecContext(ctx, `UPDATE job_run_control_plane SET phase='Running',updated_at=$1 WHERE id=$2 AND tenant_id=$3`, r.now().UTC(), runID, tenantID); err != nil {
				return err
			}
			return insertControlPlaneAudit(ctx, tx, tenantID, d.actor, auditEventRunStatusChanged, auditResourceRun, strconv.FormatInt(runID, 10), map[string]any{"from": d.phase, "to": jobrun.Running, "attempt_number": attempt.Number, "identity_confirmed": true})
		}
		if d.stage != jobrun.DispatchPrepared {
			return jobrun.ErrDispatchMayHaveStarted
		}
		if _, err = tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET dispatch_stage='DispatchMayHaveStarted',dispatch_marked_at=$1 WHERE id=$2 AND tenant_id=$3`, r.now().UTC(), d.id, tenantID); err != nil {
			return err
		}
		if err = insertControlPlaneAudit(ctx, tx, tenantID, d.actor, "attempt.dispatch_marked", auditResourceAttempt, strconv.FormatInt(d.id, 10), map[string]any{"run_id": runID, "attempt_number": attempt.Number, "job_name": attempt.KubernetesJobName}); err != nil {
			return err
		}
		claimed = true
		return nil
	}); err != nil {
		return err
	}
	if !claimed {
		return nil
	}

	// Keep parent/run/attempt locks during the one authorized external call.
	// Cancellation that committed after marking but before this lock wins.
	return r.WithTenantTransaction(lockCtx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		d, err := readAttemptDispatch(ctx, tx, tenantID, runID, attempt.Number)
		if err != nil {
			return err
		}
		if d.uid != "" {
			return nil
		}
		if d.stage != jobrun.DispatchMayHaveStarted {
			return ErrRunChanged
		}
		confirmed, err := ensure(ctx)
		if err != nil {
			return err
		}
		if confirmed.Number != attempt.Number || confirmed.KubernetesJobName != attempt.KubernetesJobName || confirmed.KubernetesJobUID == "" {
			return ErrAttemptConflict
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		var started any
		if !confirmed.StartedAt.IsZero() {
			started = confirmed.StartedAt.UTC()
		}
		if confirmed.Phase == "" {
			confirmed.Phase = jobrun.CreatingAttempt
		}
		if _, err = tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane
SET kubernetes_job_uid=$1,observed_resource_version=NULLIF($2,''),phase=$3,started_at=COALESCE(started_at,$4::timestamptz)
WHERE id=$5 AND tenant_id=$6`, confirmed.KubernetesJobUID, confirmed.ObservedResourceVersion, confirmed.Phase, started, d.id, tenantID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE job_run_control_plane SET phase='Running',updated_at=$1 WHERE id=$2 AND tenant_id=$3`, r.now().UTC(), runID, tenantID); err != nil {
			return err
		}
		return insertControlPlaneAudit(ctx, tx, tenantID, d.actor, auditEventAttemptStatusChanged, auditResourceAttempt, strconv.FormatInt(d.id, 10), map[string]any{"run_id": runID, "attempt_number": attempt.Number, "kubernetes_job_uid": confirmed.KubernetesJobUID, "identity_confirmed": true, "dispatch_stage": jobrun.DispatchBound})
	})
}

// A step may have committed before its workflow was stopped and its JobRun
// may arrive much later. Fence both reservation and external Ensure against
// the durable parent phase. Always lock parent before child/attempt rows,
// matching workflow pruning, and hold that lock throughout the external call.
func lockWorkflowParentForAttempt(ctx context.Context, tx *sql.Tx, tenantID string, runID int64) error {
	var parentID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT workflow_run_id FROM job_run_control_plane WHERE tenant_id=$1 AND id=$2`, tenantID, runID).Scan(&parentID); err != nil {
		return err
	}
	if !parentID.Valid {
		return nil
	}
	var phase string
	if err := tx.QueryRowContext(ctx, lockWorkflowRunForStepSQL, parentID.Int64, tenantID).Scan(&phase); err != nil {
		return err
	}
	if !workflowAcceptsNewSteps(phase) {
		if phase == "ExecutionUnknown" {
			return ErrWorkflowBlocked
		}
		return fmt.Errorf("workflow run %d no longer permits attempt creation: %w", parentID.Int64, ErrWorkflowStopped)
	}
	return requireWorkflowExecutionKnown(ctx, tx, tenantID, parentID.Int64)
}

// BindAttemptObservation confirms a previously uncertain creation intent.
// Callers must validate full Kubernetes ownership before supplying the UID.
// Identity can be learned during cancellation without advancing either phase.
func (r *ControlPlaneRepository) BindAttemptObservation(ctx context.Context, tenantID string, runID int64, attemptNumber int, jobName, jobUID, resourceVersion string) error {
	if runID < 1 || attemptNumber < 1 || jobName == "" || jobUID == "" {
		return fmt.Errorf("observed attempt identity is required")
	}
	return r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := lockWorkflowParentForObservation(ctx, tx, tenantID, runID); err != nil {
			return err
		}
		var id int64
		var actor, uid string
		var stage jobrun.DispatchStage
		var currentAttempt int
		if err := tx.QueryRowContext(ctx, `SELECT a.id,r.actor,COALESCE(a.kubernetes_job_uid,''),r.attempt,a.dispatch_stage
FROM job_run_attempts_control_plane a JOIN job_run_control_plane r ON r.id=a.run_id
WHERE a.tenant_id=$1 AND r.tenant_id=$1 AND a.run_id=$2 AND a.attempt_number=$3 AND a.kubernetes_job_name=$4
FOR UPDATE OF r,a`, tenantID, runID, attemptNumber, jobName).Scan(&id, &actor, &uid, &currentAttempt, &stage); err != nil {
			return err
		}
		if currentAttempt != attemptNumber {
			return ErrRunChanged
		}
		if uid != "" {
			if uid != jobUID {
				return ErrAttemptOwnership
			}
			return nil
		}
		if stage != jobrun.DispatchMayHaveStarted {
			return ErrAttemptOwnership
		}
		// Finding the original unbound dispatch resolves absence of its creation
		// acknowledgement. It does not erase a recorded Pod evidence gap.
		if _, err := tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET kubernetes_job_uid=$1,observed_resource_version=NULLIF($2,''),
 execution_unknown_since=CASE WHEN execution_unknown_reason='JobNotFound' AND termination_evidence IS NULL THEN NULL ELSE execution_unknown_since END,
 execution_unknown_reason=CASE WHEN execution_unknown_reason='JobNotFound' AND termination_evidence IS NULL THEN NULL ELSE execution_unknown_reason END
 WHERE id=$3 AND tenant_id=$4`, jobUID, resourceVersion, id, tenantID); err != nil {
			return err
		}
		return insertControlPlaneAudit(ctx, tx, tenantID, actor, "attempt.identity_bound", auditResourceAttempt, strconv.FormatInt(id, 10), map[string]any{"run_id": runID, "attempt_number": attemptNumber, "kubernetes_job_uid": jobUID})
	})
}

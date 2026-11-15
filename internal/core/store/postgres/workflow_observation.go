package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

// Observations can isolate a parent but must never reverse parent -> run locks.
func lockWorkflowParentForObservation(ctx context.Context, tx *sql.Tx, tenant string, runID int64) (sql.NullInt64, error) {
	var parent sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT workflow_run_id FROM job_run_control_plane WHERE tenant_id=$1 AND id=$2`, tenant, runID).Scan(&parent); err != nil {
		return parent, err
	}
	if parent.Valid {
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM workflow_run_control_plane WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, parent.Int64).Scan(&id); err != nil {
			return parent, err
		}
	}
	return parent, nil
}

// The caller holds the parent lock before changing its child's uncertain fact.
// Isolate new work immediately, while preserving explicit parent cancellation
// and terminal state. Existing siblings remain entitled to observation.
func isolateWorkflowForUnknown(ctx context.Context, tx *sql.Tx, tenant string, parent sql.NullInt64, at time.Time) error {
	if !parent.Valid {
		return nil
	}
	var prior, actor string
	err := tx.QueryRowContext(ctx, `WITH prior AS (
 SELECT phase,actor FROM workflow_run_control_plane WHERE tenant_id=$1 AND id=$2
)
UPDATE workflow_run_control_plane w SET phase='ExecutionUnknown',updated_at=$3
FROM prior WHERE w.tenant_id=$1 AND w.id=$2 AND w.phase IN ('Pending','Running')
RETURNING prior.phase,prior.actor`, tenant, parent.Int64, at).Scan(&prior, &actor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return insertControlPlaneAudit(ctx, tx, tenant, actor, auditEventWorkflowRunPhaseChanged,
		auditResourceWorkflowRun, strconv.FormatInt(parent.Int64, 10), map[string]any{
			"from_phase": prior, "to_phase": "ExecutionUnknown", "reason": "child_execution_unknown",
		})
}

// The parent lock also fences new work across upgrades: an older writer could
// commit child CancelUnknown before its walker updated the parent. Observe
// existing children without locking them, preserving parent -> child ordering.
func requireWorkflowExecutionKnown(ctx context.Context, tx *sql.Tx, tenant string, parentID int64) error {
	var uncertain bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM job_run_control_plane
 WHERE tenant_id=$1 AND workflow_run_id=$2 AND phase IN ('ExecutionUnknown','CancelUnknown'))`, tenant, parentID).Scan(&uncertain); err != nil {
		return err
	}
	if uncertain {
		return ErrWorkflowBlocked
	}
	return nil
}

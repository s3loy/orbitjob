package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"orbitjob/internal/core/domain/checkrun"
)

// CheckRunRepository maintains the check_runs read model.
//
// check_runs is not a work queue: check executions are Kubernetes Jobs driven
// from job_run_control_plane, and this table is derived from them (design
// section 4, Option A). It exists so the admin API's check-run surface keeps
// its shape and probe history stays queryable per check without joining the
// ledger. The one writer is the operator's terminal-phase bookkeeping.
type CheckRunRepository struct {
	db *sql.DB
}

func NewCheckRunRepository(db *sql.DB) *CheckRunRepository {
	return &CheckRunRepository{db: db}
}

const recordCompletedCheckRunSQL = `
INSERT INTO check_runs
  (run_id, tenant_id, check_id, status, scheduled_at, started_at, finished_at, duration_ms)
VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (run_id) DO NOTHING
RETURNING id`

// RecordCompleted upserts one terminal check outcome into the read model.
//
// runID is the deterministic UUID derived from the ledger's occurrence key, so
// a replayed recording lands on the same row instead of doubling history; the
// ON CONFLICT guard makes the call safe even where the caller's
// exactly-once guard already holds. severity, output and evaluation_result
// have no producer under exit-code-only evaluation and stay at their defaults.
func (r *CheckRunRepository) RecordCompleted(
	ctx context.Context, tenantID string, req checkrun.CompletedRecord,
) error {
	if req.RunID == "" {
		return fmt.Errorf("run id is required")
	}
	if req.Status != checkrun.StatusSuccess && req.Status != checkrun.StatusFailed {
		return fmt.Errorf("status %q is not a terminal check-run status", req.Status)
	}
	if !req.ScheduledAt.IsZero() && req.StartedAt.IsZero() {
		// The status/timestamps CHECK requires both instants on a terminal row.
		req.StartedAt = req.ScheduledAt
	}
	if req.FinishedAt.IsZero() {
		req.FinishedAt = time.Now().UTC()
	}
	if req.DurationMs < 0 {
		req.DurationMs = 0
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin check run record tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	var id int64
	err = tx.QueryRowContext(ctx, recordCompletedCheckRunSQL,
		req.RunID, tenantID, req.CheckID, req.Status,
		req.ScheduledAt, req.StartedAt, req.FinishedAt, req.DurationMs,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("record completed check run: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit check run record: %w", err)
	}
	return nil
}

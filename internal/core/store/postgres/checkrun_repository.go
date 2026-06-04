package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"orbitjob/internal/core/domain/checkrun"
)

type CheckRunRepository struct {
	db *sql.DB
}

func NewCheckRunRepository(db *sql.DB) *CheckRunRepository {
	return &CheckRunRepository{db: db}
}

func (r *CheckRunRepository) Create(ctx context.Context, tenantID string, checkID int64, scheduledAt time.Time) (checkrun.Snapshot, error) {
	var snap checkrun.Snapshot
	var outputRaw, evalRaw []byte
	var severity sql.NullString
	var startedAt, finishedAt sql.NullTime
	var durationMs sql.NullInt32
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO check_runs (tenant_id, check_id, status, scheduled_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, run_id::text, tenant_id, check_id, status, severity, output, evaluation_result,
		          scheduled_at, started_at, finished_at, duration_ms, version, created_at
	`, tenantID, checkID, checkrun.StatusPending, scheduledAt).Scan(
		&snap.ID, &snap.RunID, &snap.TenantID, &snap.CheckID, &snap.Status, &severity,
		&outputRaw, &evalRaw, &snap.ScheduledAt, &startedAt, &finishedAt,
		&durationMs, &snap.Version, &snap.CreatedAt,
	)
	if err != nil {
		return checkrun.Snapshot{}, fmt.Errorf("insert check_run: %w", err)
	}
	if severity.Valid {
		snap.Severity = &severity.String
	}
	if len(outputRaw) > 0 {
		_ = json.Unmarshal(outputRaw, &snap.Output)
	}
	if len(evalRaw) > 0 {
		_ = json.Unmarshal(evalRaw, &snap.EvaluationResult)
	}
	if startedAt.Valid {
		snap.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		snap.FinishedAt = &finishedAt.Time
	}
	if durationMs.Valid {
		dm := int(durationMs.Int32)
		snap.DurationMs = &dm
	}
	return snap, nil
}

func (r *CheckRunRepository) ClaimNext(ctx context.Context, tenantID string, limit int, now time.Time) ([]checkrun.Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return nil, fmt.Errorf("set tenant context: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		UPDATE check_runs
		SET status = 'running', started_at = $1
		WHERE id IN (
			SELECT id FROM check_runs
			WHERE tenant_id = $2 AND status = 'pending'
			ORDER BY scheduled_at ASC, id ASC
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, run_id::text, tenant_id, check_id, status, severity, output, evaluation_result,
		          scheduled_at, started_at, finished_at, duration_ms, version, created_at
	`, now, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("claim check_runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var runs []checkrun.Snapshot
	for rows.Next() {
		var snap checkrun.Snapshot
		var outputRaw, evalRaw []byte
		var severity sql.NullString
		var startedAt, finishedAt sql.NullTime
		var durationMs sql.NullInt32
		err := rows.Scan(
			&snap.ID, &snap.RunID, &snap.TenantID, &snap.CheckID, &snap.Status, &severity,
			&outputRaw, &evalRaw, &snap.ScheduledAt, &startedAt, &finishedAt,
			&durationMs, &snap.Version, &snap.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan check_run: %w", err)
		}
		if severity.Valid {
			snap.Severity = &severity.String
		}
		if len(outputRaw) > 0 {
			_ = json.Unmarshal(outputRaw, &snap.Output)
		}
		if len(evalRaw) > 0 {
			_ = json.Unmarshal(evalRaw, &snap.EvaluationResult)
		}
		if startedAt.Valid {
			snap.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			snap.FinishedAt = &finishedAt.Time
		}
		if durationMs.Valid {
			dm := int(durationMs.Int32)
			snap.DurationMs = &dm
		}
		runs = append(runs, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate check_runs: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim: %w", err)
	}

	return runs, nil
}

func (r *CheckRunRepository) Complete(ctx context.Context, tenantID string, id int64, status, severity string, output, evaluationResult map[string]any, durationMs int, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin complete tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	outputBytes, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}

	evalBytes, err := json.Marshal(evaluationResult)
	if err != nil {
		return fmt.Errorf("marshal evaluation_result: %w", err)
	}

	var updatedID int64
	err = tx.QueryRowContext(ctx, `
		UPDATE check_runs
		SET status = $1, severity = $2, output = $3::jsonb, evaluation_result = $4::jsonb,
		    duration_ms = $5, finished_at = $6, version = version + 1
		WHERE tenant_id = $7 AND id = $8 AND status = 'running'
		RETURNING id
	`, status, severity, outputBytes, evalBytes, durationMs, now, tenantID, id).Scan(&updatedID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("check run not found or not running: %d", id)
	}
	if err != nil {
		return fmt.Errorf("complete check_run: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit complete: %w", err)
	}
	return nil
}

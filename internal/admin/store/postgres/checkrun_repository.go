package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	checkrunquery "orbitjob/internal/admin/app/checkrun/query"
	"orbitjob/internal/core/domain/checkrun"
	"orbitjob/internal/domain/resource"
)

type CheckRunRepository struct {
	db *sql.DB
}

func NewCheckRunRepository(db *sql.DB) *CheckRunRepository {
	return &CheckRunRepository{db: db}
}

func (r *CheckRunRepository) Get(ctx context.Context, tenantID string, id int64) (checkrunquery.GetResult, error) {
	var snap checkrun.Snapshot
	var outputBytes, evalResultBytes []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT id, run_id::text, tenant_id, check_id, status, severity, output, evaluation_result,
		       scheduled_at, started_at, finished_at, duration_ms, version, created_at
		FROM check_runs
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id).Scan(
		&snap.ID, &snap.RunID, &snap.TenantID, &snap.CheckID, &snap.Status, &snap.Severity,
		&outputBytes, &evalResultBytes, &snap.ScheduledAt, &snap.StartedAt, &snap.FinishedAt,
		&snap.DurationMs, &snap.Version, &snap.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return checkrunquery.GetResult{}, &resource.NotFoundError{
			Resource: "check_run",
			ID:       id,
		}
	}
	if err != nil {
		return checkrunquery.GetResult{}, fmt.Errorf("get check_run: %w", err)
	}

	if outputBytes != nil {
		if err := json.Unmarshal(outputBytes, &snap.Output); err != nil {
			return checkrunquery.GetResult{}, fmt.Errorf("unmarshal output: %w", err)
		}
	}
	if evalResultBytes != nil {
		if err := json.Unmarshal(evalResultBytes, &snap.EvaluationResult); err != nil {
			return checkrunquery.GetResult{}, fmt.Errorf("unmarshal evaluation_result: %w", err)
		}
	}

	return toGetResult(snap), nil
}

func (r *CheckRunRepository) List(ctx context.Context, in checkrunquery.ListCheckRunsInput) ([]checkrunquery.ListItem, int, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var total int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM check_runs
		WHERE tenant_id = $1
		  AND ($2::bigint IS NULL OR check_id = $2)
		  AND ($3::text IS NULL OR status = $3)
	`, in.TenantID, in.CheckID, in.Status).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count check_runs: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, run_id::text, check_id, status, severity, duration_ms, created_at
		FROM check_runs
		WHERE tenant_id = $1
		  AND ($2::bigint IS NULL OR check_id = $2)
		  AND ($3::text IS NULL OR status = $3)
		ORDER BY created_at DESC
		LIMIT $4 OFFSET $5
	`, in.TenantID, in.CheckID, in.Status, limit, in.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list check_runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []checkrunquery.ListItem
	for rows.Next() {
		var item checkrunquery.ListItem
		var severity sql.NullString
		var durationMs sql.NullInt32
		err := rows.Scan(
			&item.ID, &item.RunID, &item.CheckID, &item.Status, &severity, &durationMs, &item.CreatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan check_run: %w", err)
		}
		if severity.Valid {
			item.Severity = &severity.String
		}
		if durationMs.Valid {
			ms := int(durationMs.Int32)
			item.DurationMs = &ms
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate check_runs: %w", err)
	}

	return items, total, nil
}

func toGetResult(snap checkrun.Snapshot) checkrunquery.GetResult {
	return checkrunquery.GetResult{
		ID:               snap.ID,
		RunID:            snap.RunID,
		TenantID:         snap.TenantID,
		CheckID:          snap.CheckID,
		Status:           snap.Status,
		Severity:         snap.Severity,
		Output:           snap.Output,
		EvaluationResult: snap.EvaluationResult,
		ScheduledAt:      snap.ScheduledAt,
		StartedAt:        snap.StartedAt,
		FinishedAt:       snap.FinishedAt,
		DurationMs:       snap.DurationMs,
		Version:          snap.Version,
		CreatedAt:        snap.CreatedAt,
	}
}

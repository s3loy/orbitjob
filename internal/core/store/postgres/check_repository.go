package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/tenant"
)

type CheckRepository struct {
	db *sql.DB
}

func NewCheckRepository(db *sql.DB) *CheckRepository {
	return &CheckRepository{db: db}
}

func (r *CheckRepository) Create(ctx context.Context, spec check.CreateSpec) (check.Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return check.Snapshot{}, fmt.Errorf("begin create tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", spec.TenantID); err != nil {
		return check.Snapshot{}, fmt.Errorf("set tenant context: %w", err)
	}

	checkConfigBytes, err := json.Marshal(spec.CheckConfig)
	if err != nil {
		return check.Snapshot{}, fmt.Errorf("marshal check_config: %w", err)
	}

	rulesBytes, err := json.Marshal(spec.AssertionRules)
	if err != nil {
		return check.Snapshot{}, fmt.Errorf("marshal assertion_rules: %w", err)
	}

	labelsBytes, err := json.Marshal(spec.Labels)
	if err != nil {
		return check.Snapshot{}, fmt.Errorf("marshal labels: %w", err)
	}

	var nextRunAt interface{}
	if spec.NextRunAt != nil {
		nextRunAt = *spec.NextRunAt
	} else {
		nextRunAt = nil
	}

	var cronExpr interface{}
	if spec.CronExpr != nil {
		cronExpr = *spec.CronExpr
	} else {
		cronExpr = nil
	}

	var intervalSec interface{}
	if spec.IntervalSec != nil {
		intervalSec = *spec.IntervalSec
	} else {
		intervalSec = nil
	}

	var description interface{}
	if spec.Description != nil {
		description = *spec.Description
	} else {
		description = nil
	}

	var snap check.Snapshot
	err = tx.QueryRowContext(ctx, `
		INSERT INTO checks (
			name, description, tenant_id, status, check_type, check_config, assertion_rules,
			schedule_type, cron_expr, interval_sec, timezone, timeout_sec, retry_limit,
			priority, labels, next_run_at
		) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11, $12, $13, $14, $15::jsonb, $16)
		RETURNING id, name, description, tenant_id, status, check_type, check_config, assertion_rules,
		          schedule_type, cron_expr, interval_sec, timezone, timeout_sec, retry_limit,
		          priority, labels, next_run_at, version, created_at, updated_at
	`, spec.Name, description, spec.TenantID, check.StatusActive, spec.CheckType,
		checkConfigBytes, rulesBytes, spec.ScheduleType, cronExpr, intervalSec,
		spec.Timezone, spec.TimeoutSec, spec.RetryLimit, spec.Priority, labelsBytes, nextRunAt,
	).Scan(
		&snap.ID, &snap.Name, &snap.Description, &snap.TenantID, &snap.Status, &snap.CheckType,
		&snap.CheckConfig, &snap.AssertionRules, &snap.ScheduleType, &snap.CronExpr, &snap.IntervalSec,
		&snap.Timezone, &snap.TimeoutSec, &snap.RetryLimit, &snap.Priority, &snap.Labels,
		&snap.NextRunAt, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if err != nil {
		return check.Snapshot{}, fmt.Errorf("insert check: %w", err)
	}

	// Audit
	diff := map[string]any{
		"name":        spec.Name,
		"check_type":  spec.CheckType,
		"schedule_type": spec.ScheduleType,
	}
	diffBytes, _ := json.Marshal(diff)
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO audit_events (tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
	`, spec.TenantID, tenant.ActorTypeSystem, "scheduler", "check.created", "check", snap.ID, diffBytes); err != nil {
		return check.Snapshot{}, fmt.Errorf("insert audit: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return check.Snapshot{}, fmt.Errorf("commit create: %w", err)
	}

	return snap, nil
}

func (r *CheckRepository) ChangeStatus(ctx context.Context, tenantID string, id int64, version int, action string) (check.Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return check.Snapshot{}, fmt.Errorf("begin status tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return check.Snapshot{}, fmt.Errorf("set tenant context: %w", err)
	}

	var nextStatus string
	switch action {
	case check.ActionPause:
		nextStatus, err = check.Pause("", version)
		if err != nil {
			return check.Snapshot{}, err
		}
	case check.ActionResume:
		nextStatus, err = check.Resume("", version)
		if err != nil {
			return check.Snapshot{}, err
		}
	default:
		return check.Snapshot{}, fmt.Errorf("unknown action: %s", action)
	}

	// Need to read current status first for validation.
	var currentStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT status FROM checks WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, id).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		return check.Snapshot{}, fmt.Errorf("check not found: %d", id)
	}
	if err != nil {
		return check.Snapshot{}, fmt.Errorf("read check status: %w", err)
	}

	switch action {
	case check.ActionPause:
		nextStatus, err = check.Pause(currentStatus, version)
	case check.ActionResume:
		nextStatus, err = check.Resume(currentStatus, version)
	}
	if err != nil {
		return check.Snapshot{}, err
	}

	var snap check.Snapshot
	err = tx.QueryRowContext(ctx, `
		UPDATE checks
		SET status = $1, version = version + 1, updated_at = now()
		WHERE tenant_id = $2 AND id = $3 AND version = $4 AND deleted_at IS NULL
		RETURNING id, name, description, tenant_id, status, check_type, check_config, assertion_rules,
		          schedule_type, cron_expr, interval_sec, timezone, timeout_sec, retry_limit,
		          priority, labels, next_run_at, version, created_at, updated_at
	`, nextStatus, tenantID, id, version).Scan(
		&snap.ID, &snap.Name, &snap.Description, &snap.TenantID, &snap.Status, &snap.CheckType,
		&snap.CheckConfig, &snap.AssertionRules, &snap.ScheduleType, &snap.CronExpr, &snap.IntervalSec,
		&snap.Timezone, &snap.TimeoutSec, &snap.RetryLimit, &snap.Priority, &snap.Labels,
		&snap.NextRunAt, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		// Check if check exists (stale version vs not found).
		var existingID int64
		if err := tx.QueryRowContext(ctx, "SELECT id FROM checks WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL", tenantID, id).Scan(&existingID); err == sql.ErrNoRows {
			return check.Snapshot{}, fmt.Errorf("check not found: %d", id)
		}
		return check.Snapshot{}, fmt.Errorf("check version conflict: %d", id)
	}
	if err != nil {
		return check.Snapshot{}, fmt.Errorf("update check status: %w", err)
	}

	// Audit
	diff := map[string]any{
		"from_status": currentStatus,
		"to_status":   nextStatus,
		"from_version": version,
		"to_version":  snap.Version,
	}
	diffBytes, _ := json.Marshal(diff)
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO audit_events (tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
	`, tenantID, tenant.ActorTypeSystem, "scheduler", "check.status_changed", "check", id, diffBytes); err != nil {
		return check.Snapshot{}, fmt.Errorf("insert audit: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return check.Snapshot{}, fmt.Errorf("commit status: %w", err)
	}

	return snap, nil
}

func (r *CheckRepository) Delete(ctx context.Context, tenantID string, id int64, version int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	var deletedID int64
	err = tx.QueryRowContext(ctx, `
		UPDATE checks
		SET deleted_at = now(), version = version + 1, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND version = $3 AND deleted_at IS NULL
		RETURNING id
	`, tenantID, id, version).Scan(&deletedID)
	if err == sql.ErrNoRows {
		var existingID int64
		if err := tx.QueryRowContext(ctx, "SELECT id FROM checks WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL", tenantID, id).Scan(&existingID); err == sql.ErrNoRows {
			return fmt.Errorf("check not found: %d", id)
		}
		return fmt.Errorf("check version conflict: %d", id)
	}
	if err != nil {
		return fmt.Errorf("delete check: %w", err)
	}

	// Audit
	diff := map[string]any{"action": "delete", "from_version": version}
	diffBytes, _ := json.Marshal(diff)
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO audit_events (tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
	`, tenantID, tenant.ActorTypeSystem, "scheduler", "check.deleted", "check", id, diffBytes); err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit delete: %w", err)
	}

	return nil
}

func (r *CheckRepository) ListDue(ctx context.Context, tenantID string, now time.Time, limit int) ([]check.Snapshot, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, description, tenant_id, status, check_type, check_config, assertion_rules,
		       schedule_type, cron_expr, interval_sec, timezone, timeout_sec, retry_limit,
		       priority, labels, next_run_at, version, created_at, updated_at
		FROM checks
		WHERE tenant_id = $1 AND status = 'active' AND next_run_at <= $2 AND deleted_at IS NULL
		ORDER BY next_run_at ASC, priority DESC
		LIMIT $3
	`, tenantID, now, limit)
	if err != nil {
		return nil, fmt.Errorf("list due checks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var checks []check.Snapshot
	for rows.Next() {
		var snap check.Snapshot
		err := rows.Scan(
			&snap.ID, &snap.Name, &snap.Description, &snap.TenantID, &snap.Status, &snap.CheckType,
			&snap.CheckConfig, &snap.AssertionRules, &snap.ScheduleType, &snap.CronExpr, &snap.IntervalSec,
			&snap.Timezone, &snap.TimeoutSec, &snap.RetryLimit, &snap.Priority, &snap.Labels,
			&snap.NextRunAt, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan check: %w", err)
		}
		checks = append(checks, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate checks: %w", err)
	}

	return checks, nil
}

func (r *CheckRepository) UpdateNextRunAt(ctx context.Context, tenantID string, id int64, nextRunAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE checks SET next_run_at = $1, updated_at = now()
		WHERE tenant_id = $2 AND id = $3 AND deleted_at IS NULL
	`, nextRunAt, tenantID, id)
	if err != nil {
		return fmt.Errorf("update next_run_at: %w", err)
	}
	return nil
}

func (r *CheckRepository) GetByID(ctx context.Context, tenantID string, id int64) (check.Snapshot, error) {
	var snap check.Snapshot
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, description, tenant_id, status, check_type, check_config, assertion_rules,
		       schedule_type, cron_expr, interval_sec, timezone, timeout_sec, retry_limit,
		       priority, labels, next_run_at, version, created_at, updated_at
		FROM checks
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, id).Scan(
		&snap.ID, &snap.Name, &snap.Description, &snap.TenantID, &snap.Status, &snap.CheckType,
		&snap.CheckConfig, &snap.AssertionRules, &snap.ScheduleType, &snap.CronExpr, &snap.IntervalSec,
		&snap.Timezone, &snap.TimeoutSec, &snap.RetryLimit, &snap.Priority, &snap.Labels,
		&snap.NextRunAt, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return check.Snapshot{}, fmt.Errorf("check not found: %d", id)
	}
	if err != nil {
		return check.Snapshot{}, fmt.Errorf("get check: %w", err)
	}
	return snap, nil
}


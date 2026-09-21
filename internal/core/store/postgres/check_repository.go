package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/tenant"
	"orbitjob/internal/domain/resource"
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
	// Unconditional release: no return path below is required to keep err
	// non-nil, so a rollback keyed on it would strand the tx.
	defer func() { _ = tx.Rollback() }()

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

	var nextRunAtParam interface{}
	if spec.NextRunAt != nil {
		nextRunAtParam = *spec.NextRunAt
	}

	var cronExpr interface{}
	if spec.CronExpr != nil {
		cronExpr = *spec.CronExpr
	}

	var intervalSec interface{}
	if spec.IntervalSec != nil {
		intervalSec = *spec.IntervalSec
	}

	var description interface{}
	if spec.Description != nil {
		description = *spec.Description
	}

	var snap check.Snapshot
	var checkConfigRaw, assertionRulesRaw, labelsRaw []byte
	var nextRunAt sql.NullTime
	var resourceGroup sql.NullString
	err = tx.QueryRowContext(ctx, `
		INSERT INTO checks (
			name, description, tenant_id, status, check_type, check_config, assertion_rules,
			schedule_type, cron_expr, interval_sec, timezone, timeout_sec, retry_limit,
			priority, labels, next_run_at, resource_group_id
		) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11, $12, $13, $14, $15::jsonb, $16, $17)
		RETURNING id, name, description, tenant_id, resource_group_id, status, check_type, check_config, assertion_rules,
		          schedule_type, cron_expr, interval_sec, timezone, timeout_sec, retry_limit,
		          priority, labels, next_run_at, version, created_at, updated_at
	`, spec.Name, description, spec.TenantID, check.StatusActive, spec.CheckType,
		checkConfigBytes, rulesBytes, spec.ScheduleType, cronExpr, intervalSec,
		spec.Timezone, spec.TimeoutSec, spec.RetryLimit, spec.Priority, labelsBytes, nextRunAtParam,
		nullableGroup(spec.ResourceGroupID),
	).Scan(
		&snap.ID, &snap.Name, &snap.Description, &snap.TenantID, &resourceGroup, &snap.Status, &snap.CheckType,
		&checkConfigRaw, &assertionRulesRaw, &snap.ScheduleType, &snap.CronExpr, &snap.IntervalSec,
		&snap.Timezone, &snap.TimeoutSec, &snap.RetryLimit, &snap.Priority, &labelsRaw,
		&nextRunAt, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if err != nil {
		return check.Snapshot{}, fmt.Errorf("insert check: %w", err)
	}
	if len(checkConfigRaw) > 0 {
		_ = json.Unmarshal(checkConfigRaw, &snap.CheckConfig)
	}
	if len(assertionRulesRaw) > 0 {
		_ = json.Unmarshal(assertionRulesRaw, &snap.AssertionRules)
	}
	if len(labelsRaw) > 0 {
		_ = json.Unmarshal(labelsRaw, &snap.Labels)
	}
	if nextRunAt.Valid {
		snap.NextRunAt = &nextRunAt.Time
	}
	snap.ResourceGroupID = resourceGroup.String

	// Audit
	diff := map[string]any{
		"name":          spec.Name,
		"check_type":    spec.CheckType,
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

func (r *CheckRepository) ChangeStatus(ctx context.Context, tenantID, resourceGroupID string, id int64, version int, action string) (check.Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return check.Snapshot{}, fmt.Errorf("begin status tx: %w", err)
	}
	// Unconditional release: the unknown-action return below leaves err nil,
	// so a rollback keyed on err leaked the tx holding the tenant GUC.
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return check.Snapshot{}, fmt.Errorf("set tenant context: %w", err)
	}

	// Read current status first for validation.
	var currentStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT status FROM checks
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
		  AND ($3::text IS NULL OR resource_group_id = $3)
	`, tenantID, id, nullableGroup(resourceGroupID)).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		return check.Snapshot{}, &resource.NotFoundError{Resource: "check", ID: id}
	}
	if err != nil {
		return check.Snapshot{}, fmt.Errorf("read check status: %w", err)
	}

	var nextStatus string
	switch action {
	case check.ActionPause:
		nextStatus, err = check.Pause(currentStatus, version)
	case check.ActionResume:
		nextStatus, err = check.Resume(currentStatus, version)
	default:
		return check.Snapshot{}, fmt.Errorf("unknown action: %s", action)
	}
	if err != nil {
		return check.Snapshot{}, err
	}

	var snap check.Snapshot
	var checkConfigRaw, assertionRulesRaw, labelsRaw []byte
	var nextRunAt sql.NullTime
	var resourceGroup sql.NullString
	err = tx.QueryRowContext(ctx, `
		UPDATE checks
		SET status = $1, version = version + 1, updated_at = now()
		WHERE tenant_id = $2 AND id = $3 AND version = $4 AND deleted_at IS NULL
		RETURNING id, name, description, tenant_id, resource_group_id, status, check_type, check_config, assertion_rules,
		          schedule_type, cron_expr, interval_sec, timezone, timeout_sec, retry_limit,
		          priority, labels, next_run_at, version, created_at, updated_at
	`, nextStatus, tenantID, id, version).Scan(
		&snap.ID, &snap.Name, &snap.Description, &snap.TenantID, &resourceGroup, &snap.Status, &snap.CheckType,
		&checkConfigRaw, &assertionRulesRaw, &snap.ScheduleType, &snap.CronExpr, &snap.IntervalSec,
		&snap.Timezone, &snap.TimeoutSec, &snap.RetryLimit, &snap.Priority, &labelsRaw,
		&nextRunAt, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if len(checkConfigRaw) > 0 {
		_ = json.Unmarshal(checkConfigRaw, &snap.CheckConfig)
	}
	if len(assertionRulesRaw) > 0 {
		_ = json.Unmarshal(assertionRulesRaw, &snap.AssertionRules)
	}
	if len(labelsRaw) > 0 {
		_ = json.Unmarshal(labelsRaw, &snap.Labels)
	}
	if nextRunAt.Valid {
		snap.NextRunAt = &nextRunAt.Time
	}
	snap.ResourceGroupID = resourceGroup.String
	if err == sql.ErrNoRows {
		// Check if check exists (stale version vs not found).
		var existingID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM checks
			WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
			  AND ($3::text IS NULL OR resource_group_id = $3)
		`, tenantID, id, nullableGroup(resourceGroupID)).Scan(&existingID); err == sql.ErrNoRows {
			return check.Snapshot{}, &resource.NotFoundError{Resource: "check", ID: id}
		}
		return check.Snapshot{}, &resource.ConflictError{Resource: "check", ID: id, Field: "version", Message: "stale check version"}
	}
	if err != nil {
		return check.Snapshot{}, fmt.Errorf("update check status: %w", err)
	}

	// Audit
	diff := map[string]any{
		"from_status":  currentStatus,
		"to_status":    nextStatus,
		"from_version": version,
		"to_version":   snap.Version,
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

func (r *CheckRepository) Delete(ctx context.Context, tenantID, resourceGroupID string, id int64, version int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete tx: %w", err)
	}
	// Unconditional release: no return path below is required to keep err
	// non-nil, so a rollback keyed on it would strand the tx.
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	var deletedID int64
	err = tx.QueryRowContext(ctx, `
		UPDATE checks
		SET deleted_at = now(), version = version + 1, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND version = $3 AND deleted_at IS NULL
		  AND ($4::text IS NULL OR resource_group_id = $4)
		RETURNING id
	`, tenantID, id, version, nullableGroup(resourceGroupID)).Scan(&deletedID)
	if err == sql.ErrNoRows {
		var existingID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM checks
			WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
			  AND ($3::text IS NULL OR resource_group_id = $3)
		`, tenantID, id, nullableGroup(resourceGroupID)).Scan(&existingID); err == sql.ErrNoRows {
			return &resource.NotFoundError{Resource: "check", ID: id}
		}
		return &resource.ConflictError{Resource: "check", ID: id, Field: "version", Message: "stale check version"}
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

// ListDue scans one tenant's due checks inside a tenant-scoped transaction:
// row level security hides every row unless app.tenant_id is set on the
// session, so this cannot borrow a pooled connection and query directly. The
// tenant_id predicate is kept because defense in depth is cheap; the RLS
// policy is what makes cross-tenant reads impossible.
func (r *CheckRepository) ListDue(ctx context.Context, tenantID string, now time.Time, limit int) ([]check.Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin list due tx: %w", err)
	}
	// Unconditional release: the scan and rows.Err checks below capture their
	// errors into shadowed locals, so a rollback keyed on err leaked the tx
	// mid-iteration.
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return nil, fmt.Errorf("set tenant context: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, name, description, tenant_id, resource_group_id, status, check_type, check_config, assertion_rules,
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
		var checkConfigRaw, assertionRulesRaw, labelsRaw []byte
		var nextRunAt sql.NullTime
		var resourceGroup sql.NullString
		err := rows.Scan(
			&snap.ID, &snap.Name, &snap.Description, &snap.TenantID, &resourceGroup, &snap.Status, &snap.CheckType,
			&checkConfigRaw, &assertionRulesRaw, &snap.ScheduleType, &snap.CronExpr, &snap.IntervalSec,
			&snap.Timezone, &snap.TimeoutSec, &snap.RetryLimit, &snap.Priority, &labelsRaw,
			&nextRunAt, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan check: %w", err)
		}
		if len(checkConfigRaw) > 0 {
			_ = json.Unmarshal(checkConfigRaw, &snap.CheckConfig)
		}
		if len(assertionRulesRaw) > 0 {
			_ = json.Unmarshal(assertionRulesRaw, &snap.AssertionRules)
		}
		if len(labelsRaw) > 0 {
			_ = json.Unmarshal(labelsRaw, &snap.Labels)
		}
		if nextRunAt.Valid {
			snap.NextRunAt = &nextRunAt.Time
		}
		snap.ResourceGroupID = resourceGroup.String
		checks = append(checks, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate checks: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit list due: %w", err)
	}

	return checks, nil
}

// UpdateNextRunAt advances the check's scheduling cursor in a tenant-scoped
// transaction, for the same RLS reason as ListDue: an unscoped UPDATE matches
// no rows the policy does not reveal.
func (r *CheckRepository) UpdateNextRunAt(ctx context.Context, tenantID string, id int64, nextRunAt time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update next_run_at tx: %w", err)
	}
	// Unconditional release: no return path below is required to keep err
	// non-nil, so a rollback keyed on it would strand the tx.
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE checks SET next_run_at = $1, updated_at = now()
		WHERE tenant_id = $2 AND id = $3 AND deleted_at IS NULL
	`, nextRunAt, tenantID, id)
	if err != nil {
		return fmt.Errorf("update next_run_at: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read next_run_at update: %w", err)
	}
	if updated == 0 {
		return &resource.NotFoundError{Resource: "check", ID: id}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit update next_run_at: %w", err)
	}
	return nil
}

// GetByID reads one check inside a tenant-scoped transaction. The checks table
// carries row level security bound to the app.tenant_id GUC, so a read on the
// bare pool would see no rows under the runtime role and report a check that
// exists as missing -- which the terminal-outcome bookkeeping would then label
// with the unknown check type reserved for deleted definitions.
func (r *CheckRepository) GetByID(ctx context.Context, tenantID string, id int64) (check.Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return check.Snapshot{}, fmt.Errorf("begin get tx: %w", err)
	}
	// Unconditional release: the scan below captures its error into scanErr,
	// so the not-found and scan-failure returns leave err nil and a rollback
	// keyed on err would leak the transaction holding the tenant GUC.
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return check.Snapshot{}, fmt.Errorf("set tenant context: %w", err)
	}

	var snap check.Snapshot
	var checkConfigRaw, assertionRulesRaw, labelsRaw []byte
	var nextRunAt sql.NullTime
	var resourceGroup sql.NullString
	scanErr := tx.QueryRowContext(ctx, `
		SELECT id, name, description, tenant_id, resource_group_id, status, check_type, check_config, assertion_rules,
		       schedule_type, cron_expr, interval_sec, timezone, timeout_sec, retry_limit,
		       priority, labels, next_run_at, version, created_at, updated_at
		FROM checks
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, id).Scan(
		&snap.ID, &snap.Name, &snap.Description, &snap.TenantID, &resourceGroup, &snap.Status, &snap.CheckType,
		&checkConfigRaw, &assertionRulesRaw, &snap.ScheduleType, &snap.CronExpr, &snap.IntervalSec,
		&snap.Timezone, &snap.TimeoutSec, &snap.RetryLimit, &snap.Priority, &labelsRaw,
		&nextRunAt, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if scanErr == sql.ErrNoRows {
		return check.Snapshot{}, &resource.NotFoundError{Resource: "check", ID: id}
	}
	if scanErr != nil {
		return check.Snapshot{}, fmt.Errorf("get check: %w", scanErr)
	}
	if len(checkConfigRaw) > 0 {
		_ = json.Unmarshal(checkConfigRaw, &snap.CheckConfig)
	}
	if len(assertionRulesRaw) > 0 {
		_ = json.Unmarshal(assertionRulesRaw, &snap.AssertionRules)
	}
	if len(labelsRaw) > 0 {
		_ = json.Unmarshal(labelsRaw, &snap.Labels)
	}
	if nextRunAt.Valid {
		snap.NextRunAt = &nextRunAt.Time
	}
	snap.ResourceGroupID = resourceGroup.String

	if err = tx.Commit(); err != nil {
		return check.Snapshot{}, fmt.Errorf("commit get check: %w", err)
	}
	return snap, nil
}

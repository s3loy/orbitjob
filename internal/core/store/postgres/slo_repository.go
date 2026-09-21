package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/resource"
)

// SLORepository provides write-side access to slos table.
//
// Every method runs inside a transaction carrying app.tenant_id: the slos RLS
// policy compares tenant_id against that GUC and fails closed when it is
// unset, so a query issued on the bare pool is denied outright (writes) or
// sees nothing (reads) for any non-owner role. classifyWriteMiss runs on the
// same transaction for the same reason: deciding whether a missed update was
// a stale version requires seeing the row.
type SLORepository struct {
	db *sql.DB
}

// NewSLORepository creates a new SLO repository.
func NewSLORepository(db *sql.DB) *SLORepository {
	return &SLORepository{db: db}
}

// Create inserts a new SLO and returns its snapshot.
func (r *SLORepository) Create(ctx context.Context, tenantID string, spec slo.CreateSpec) (slo.Snapshot, error) {
	var snap slo.Snapshot
	var windowSecs int64

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return snap, fmt.Errorf("begin slo create tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return snap, fmt.Errorf("set tenant context: %w", err)
	}

	err = tx.QueryRowContext(ctx, `
		INSERT INTO slos (tenant_id, name, description, sli_id, target, window_type, window_duration, alert_fast_burn_rate, alert_slow_burn_rate, resource_group_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, tenant_id, name, description, sli_id, target, window_type, window_duration,
		          alert_fast_burn_rate, alert_slow_burn_rate, status, version, created_at, updated_at
	`, tenantID, spec.Name, spec.Description, spec.SLIID, spec.Target, spec.WindowType,
		spec.WindowDuration/time.Second, spec.AlertFastBurnRate, spec.AlertSlowBurnRate,
		nullableGroup(spec.ResourceGroupID)).Scan(
		&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIID, &snap.Target,
		&snap.WindowType, &windowSecs, &snap.AlertFastBurnRate, &snap.AlertSlowBurnRate,
		&snap.Status, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if err != nil {
		return snap, fmt.Errorf("insert slo: %w", err)
	}
	snap.WindowDuration = time.Duration(windowSecs) * time.Second

	if err := tx.Commit(); err != nil {
		return snap, fmt.Errorf("commit create slo: %w", err)
	}

	return snap, nil
}

// ChangeStatus updates an SLO's status.
func (r *SLORepository) ChangeStatus(ctx context.Context, tenantID, resourceGroupID string, id int64, version int, status string) (slo.Snapshot, error) {
	var snap slo.Snapshot
	var windowSecs int64

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return snap, fmt.Errorf("begin slo status tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return snap, fmt.Errorf("set tenant context: %w", err)
	}

	err = tx.QueryRowContext(ctx, `
		UPDATE slos SET status = $1, version = version + 1, updated_at = now()
		WHERE tenant_id = $2 AND id = $3 AND version = $4 AND deleted_at IS NULL
		  AND ($5::text IS NULL OR resource_group_id = $5)
		RETURNING id, tenant_id, name, description, sli_id, target, window_type, window_duration,
		          alert_fast_burn_rate, alert_slow_burn_rate, status, version, created_at, updated_at
	`, status, tenantID, id, version, nullableGroup(resourceGroupID)).Scan(
		&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIID, &snap.Target,
		&snap.WindowType, &windowSecs, &snap.AlertFastBurnRate, &snap.AlertSlowBurnRate,
		&snap.Status, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return snap, r.classifyWriteMiss(ctx, tx, tenantID, resourceGroupID, id)
	}
	if err != nil {
		return snap, fmt.Errorf("update slo status: %w", err)
	}
	snap.WindowDuration = time.Duration(windowSecs) * time.Second

	if err := tx.Commit(); err != nil {
		return snap, fmt.Errorf("commit slo status: %w", err)
	}

	return snap, nil
}

// Delete soft-deletes an SLO.
func (r *SLORepository) Delete(ctx context.Context, tenantID, resourceGroupID string, id int64, version int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin slo delete tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE slos SET deleted_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND version = $3 AND deleted_at IS NULL
		  AND ($4::text IS NULL OR resource_group_id = $4)
	`, tenantID, id, version, nullableGroup(resourceGroupID))
	if err != nil {
		return fmt.Errorf("delete slo: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return r.classifyWriteMiss(ctx, tx, tenantID, resourceGroupID, id)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete slo: %w", err)
	}

	return nil
}

// classifyWriteMiss tells a version conflict apart from a row the caller cannot
// reach.
//
// Reporting both as one conflict makes the two indistinguishable, which is
// safe but unhelpful: a caller that sent a stale version is told its SLO is
// gone, and one whose SLO is merely outside its resource group gets advice
// about versions. The lookup carries the scope, so a row in another group reads
// as absent, exactly as it does on the read path.
//
// It runs on the caller's transaction: the miss must be classified in the same
// tenant context that missed, or the lookup sees nothing and every conflict
// reports as not-found.
func (r *SLORepository) classifyWriteMiss(ctx context.Context, tx *sql.Tx, tenantID, resourceGroupID string, id int64) error {
	var existingID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM slos
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
		  AND ($3::text IS NULL OR resource_group_id = $3)
	`, tenantID, id, nullableGroup(resourceGroupID)).Scan(&existingID)
	if err == sql.ErrNoRows {
		return &resource.NotFoundError{Resource: "slo", ID: id}
	}
	if err != nil {
		return fmt.Errorf("classify slo write miss: %w", err)
	}
	return &resource.ConflictError{Resource: "slo", ID: id, Field: "version", Message: "stale slo version"}
}

// ListActive returns all active SLOs for a tenant.
func (r *SLORepository) ListActive(ctx context.Context, tenantID string) ([]slo.Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin slo list tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return nil, fmt.Errorf("set tenant context: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, tenant_id, name, description, sli_id, target, window_type, window_duration,
		       alert_fast_burn_rate, alert_slow_burn_rate, status, version, created_at, updated_at
		FROM slos
		WHERE tenant_id = $1 AND status = 'active' AND deleted_at IS NULL
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query active slos: %w", err)
	}
	defer func() { _ = rows.Close() }()

	slos, err := r.scanSLOs(rows)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit list slos: %w", err)
	}

	return slos, nil
}

func (r *SLORepository) scanSLOs(rows *sql.Rows) ([]slo.Snapshot, error) {
	var slos []slo.Snapshot
	for rows.Next() {
		var snap slo.Snapshot
		var windowSecs int64
		err := rows.Scan(
			&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIID, &snap.Target,
			&snap.WindowType, &windowSecs, &snap.AlertFastBurnRate, &snap.AlertSlowBurnRate,
			&snap.Status, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan slo: %w", err)
		}
		snap.WindowDuration = time.Duration(windowSecs) * time.Second
		slos = append(slos, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate slos: %w", err)
	}
	return slos, nil
}

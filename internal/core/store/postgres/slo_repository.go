package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/resource"
)

// SLORepository provides write-side access to slos table.
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

	err := r.db.QueryRowContext(ctx, `
		INSERT INTO slos (tenant_id, name, description, sli_id, target, window_type, window_duration, alert_fast_burn_rate, alert_slow_burn_rate)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, tenant_id, name, description, sli_id, target, window_type, window_duration,
		          alert_fast_burn_rate, alert_slow_burn_rate, status, version, created_at, updated_at
	`, tenantID, spec.Name, spec.Description, spec.SLIID, spec.Target, spec.WindowType,
		spec.WindowDuration, spec.AlertFastBurnRate, spec.AlertSlowBurnRate).Scan(
		&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIID, &snap.Target,
		&snap.WindowType, &snap.WindowDuration, &snap.AlertFastBurnRate, &snap.AlertSlowBurnRate,
		&snap.Status, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if err != nil {
		return snap, fmt.Errorf("insert slo: %w", err)
	}

	return snap, nil
}

// ChangeStatus updates an SLO's status.
func (r *SLORepository) ChangeStatus(ctx context.Context, tenantID string, id int64, version int, status string) (slo.Snapshot, error) {
	var snap slo.Snapshot

	err := r.db.QueryRowContext(ctx, `
		UPDATE slos SET status = $1, version = version + 1, updated_at = now()
		WHERE tenant_id = $2 AND id = $3 AND version = $4 AND deleted_at IS NULL
		RETURNING id, tenant_id, name, description, sli_id, target, window_type, window_duration,
		          alert_fast_burn_rate, alert_slow_burn_rate, status, version, created_at, updated_at
	`, status, tenantID, id, version).Scan(
		&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIID, &snap.Target,
		&snap.WindowType, &snap.WindowDuration, &snap.AlertFastBurnRate, &snap.AlertSlowBurnRate,
		&snap.Status, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return snap, &resource.ConflictError{Resource: "slo", ID: id, Message: "slo not found or version conflict"}
	}
	if err != nil {
		return snap, fmt.Errorf("update slo status: %w", err)
	}

	return snap, nil
}

// Delete soft-deletes an SLO.
func (r *SLORepository) Delete(ctx context.Context, tenantID string, id int64, version int) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE slos SET deleted_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND version = $3 AND deleted_at IS NULL
	`, tenantID, id, version)
	if err != nil {
		return fmt.Errorf("delete slo: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return &resource.ConflictError{Resource: "slo", ID: id, Message: "slo not found or version conflict"}
	}

	return nil
}

// ListActive returns all active SLOs for a tenant.
func (r *SLORepository) ListActive(ctx context.Context, tenantID string) ([]slo.Snapshot, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, name, description, sli_id, target, window_type, window_duration,
		       alert_fast_burn_rate, alert_slow_burn_rate, status, version, created_at, updated_at
		FROM slos
		WHERE tenant_id = $1 AND status = 'active' AND deleted_at IS NULL
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query active slos: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return r.scanSLOs(rows)
}

func (r *SLORepository) scanSLOs(rows *sql.Rows) ([]slo.Snapshot, error) {
	var slos []slo.Snapshot
	for rows.Next() {
		var snap slo.Snapshot
		err := rows.Scan(
			&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIID, &snap.Target,
			&snap.WindowType, &snap.WindowDuration, &snap.AlertFastBurnRate, &snap.AlertSlowBurnRate,
			&snap.Status, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan slo: %w", err)
		}
		slos = append(slos, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate slos: %w", err)
	}
	return slos, nil
}

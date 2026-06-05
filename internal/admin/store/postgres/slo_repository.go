package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"orbitjob/internal/core/domain/slo"
)

// SLOReadRepository provides read-side access to slos table.
type SLOReadRepository struct {
	db *sql.DB
}

// NewSLOReadRepository creates a new read repository.
func NewSLOReadRepository(db *sql.DB) *SLOReadRepository {
	return &SLOReadRepository{db: db}
}

// Get retrieves an SLO by ID.
func (r *SLOReadRepository) Get(ctx context.Context, tenantID string, id int64) (slo.Snapshot, error) {
	var snap slo.Snapshot

	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, description, sli_id, target, window_type, window_duration,
		       alert_fast_burn_rate, alert_slow_burn_rate, status, version, created_at, updated_at
		FROM slos
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, id).Scan(
		&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIID, &snap.Target,
		&snap.WindowType, &snap.WindowDuration, &snap.AlertFastBurnRate, &snap.AlertSlowBurnRate,
		&snap.Status, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return snap, fmt.Errorf("slo not found: %d", id)
	}
	if err != nil {
		return snap, fmt.Errorf("get slo: %w", err)
	}

	return snap, nil
}

// List retrieves a paginated list of SLOs.
func (r *SLOReadRepository) List(ctx context.Context, tenantID string, limit, offset int) ([]slo.Snapshot, int64, error) {
	var total int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM slos WHERE tenant_id = $1 AND deleted_at IS NULL
	`, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count slos: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, name, description, sli_id, target, window_type, window_duration,
		       alert_fast_burn_rate, alert_slow_burn_rate, status, version, created_at, updated_at
		FROM slos
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY id DESC
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list slos: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var slos []slo.Snapshot
	for rows.Next() {
		var snap slo.Snapshot
		err := rows.Scan(
			&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIID, &snap.Target,
			&snap.WindowType, &snap.WindowDuration, &snap.AlertFastBurnRate, &snap.AlertSlowBurnRate,
			&snap.Status, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan slo: %w", err)
		}
		slos = append(slos, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate slos: %w", err)
	}

	return slos, total, nil
}

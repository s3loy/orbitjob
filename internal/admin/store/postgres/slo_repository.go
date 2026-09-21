package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/resource"
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
func (r *SLOReadRepository) Get(ctx context.Context, tenantID, resourceGroupID string, id int64) (slo.Snapshot, error) {
	var snap slo.Snapshot
	var windowSecs int64

	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return snap, fmt.Errorf("begin slo get tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx, `
			SELECT id, tenant_id, name, description, sli_id, target, window_type, window_duration,
			       alert_fast_burn_rate, alert_slow_burn_rate, status, version, created_at, updated_at
			FROM slos
			WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
			  AND ($3::text IS NULL OR resource_group_id = $3)
		`, tenantID, id, nullableGroup(resourceGroupID)).Scan(
		&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIID, &snap.Target,
		&snap.WindowType, &windowSecs, &snap.AlertFastBurnRate, &snap.AlertSlowBurnRate,
		&snap.Status, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return snap, &resource.NotFoundError{Resource: "slo", ID: id}
	}
	if err != nil {
		return snap, fmt.Errorf("get slo: %w", err)
	}
	snap.WindowDuration = time.Duration(windowSecs) * time.Second

	_ = tx.Commit()
	return snap, nil
}

// List retrieves a paginated list of SLOs.
func (r *SLOReadRepository) List(ctx context.Context, tenantID, resourceGroupID string, limit, offset int) ([]slo.Snapshot, int64, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("begin slo list tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The group predicate belongs in the query, not in a filter over the page:
	// post-filtering would report a total that counts rows the caller may not
	// see.
	var total int64
	if err = tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM slos
			WHERE tenant_id = $1 AND deleted_at IS NULL
			  AND ($2::text IS NULL OR resource_group_id = $2)
		`, tenantID, nullableGroup(resourceGroupID)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count slos: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
			SELECT id, tenant_id, name, description, sli_id, target, window_type, window_duration,
			       alert_fast_burn_rate, alert_slow_burn_rate, status, version, created_at, updated_at
			FROM slos
			WHERE tenant_id = $1 AND deleted_at IS NULL
			  AND ($2::text IS NULL OR resource_group_id = $2)
			ORDER BY id DESC
			LIMIT $3 OFFSET $4
		`, tenantID, nullableGroup(resourceGroupID), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list slos: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
			return nil, 0, fmt.Errorf("scan slo: %w", err)
		}
		snap.WindowDuration = time.Duration(windowSecs) * time.Second
		slos = append(slos, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate slos: %w", err)
	}

	_ = tx.Commit()
	return slos, total, nil
}

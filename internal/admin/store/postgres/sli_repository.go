package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"orbitjob/internal/core/domain/sli"
)

// SLIReadRepository provides read-side access to slis table.
type SLIReadRepository struct {
	db *sql.DB
}

// NewSLIReadRepository creates a new read repository.
func NewSLIReadRepository(db *sql.DB) *SLIReadRepository {
	return &SLIReadRepository{db: db}
}

// Get retrieves an SLI by ID.
func (r *SLIReadRepository) Get(ctx context.Context, tenantID string, id int64) (sli.Snapshot, error) {
	var snap sli.Snapshot
	var sourceConfigRaw, goodEventRaw []byte

	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, description, sli_type, source_type, source_config, aggregation, good_event_criteria, version, created_at, updated_at
		FROM slis
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, id).Scan(
		&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIType, &snap.SourceType,
		&sourceConfigRaw, &snap.Aggregation, &goodEventRaw, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return snap, fmt.Errorf("sli not found: %d", id)
	}
	if err != nil {
		return snap, fmt.Errorf("get sli: %w", err)
	}

	if len(sourceConfigRaw) > 0 {
		_ = json.Unmarshal(sourceConfigRaw, &snap.SourceConfig)
	}
	if len(goodEventRaw) > 0 {
		_ = json.Unmarshal(goodEventRaw, &snap.GoodEventCriteria)
	}

	return snap, nil
}

// List retrieves a paginated list of SLIs.
func (r *SLIReadRepository) List(ctx context.Context, tenantID string, limit, offset int) ([]sli.Snapshot, int64, error) {
	var total int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM slis WHERE tenant_id = $1 AND deleted_at IS NULL
	`, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count slis: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, name, description, sli_type, source_type, source_config, aggregation, good_event_criteria, version, created_at, updated_at
		FROM slis
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY id DESC
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list slis: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var slis []sli.Snapshot
	for rows.Next() {
		var snap sli.Snapshot
		var sourceConfigRaw, goodEventRaw []byte
		err := rows.Scan(
			&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIType, &snap.SourceType,
			&sourceConfigRaw, &snap.Aggregation, &goodEventRaw, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan sli: %w", err)
		}
		if len(sourceConfigRaw) > 0 {
			_ = json.Unmarshal(sourceConfigRaw, &snap.SourceConfig)
		}
		if len(goodEventRaw) > 0 {
			_ = json.Unmarshal(goodEventRaw, &snap.GoodEventCriteria)
		}
		slis = append(slis, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate slis: %w", err)
	}

	return slis, total, nil
}

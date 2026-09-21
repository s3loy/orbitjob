package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/domain/resource"
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
func (r *SLIReadRepository) Get(ctx context.Context, tenantID, resourceGroupID string, id int64) (sli.Snapshot, error) {
	var snap sli.Snapshot
	var sourceConfigRaw, goodEventRaw []byte

	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return snap, fmt.Errorf("begin sli get tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx, `
			SELECT id, tenant_id, name, description, sli_type, source_type, source_config, aggregation, good_event_criteria, version, created_at, updated_at
			FROM slis
			WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
			  AND ($3::text IS NULL OR resource_group_id = $3)
		`, tenantID, id, nullableGroup(resourceGroupID)).Scan(
		&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIType, &snap.SourceType,
		&sourceConfigRaw, &snap.Aggregation, &goodEventRaw, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return snap, &resource.NotFoundError{Resource: "sli", ID: id}
	}
	if err != nil {
		return snap, fmt.Errorf("get sli: %w", err)
	}

	if len(sourceConfigRaw) > 0 {
		if err := json.Unmarshal(sourceConfigRaw, &snap.SourceConfig); err != nil {
			return snap, fmt.Errorf("unmarshal source_config: %w", err)
		}
	}
	if len(goodEventRaw) > 0 {
		if err := json.Unmarshal(goodEventRaw, &snap.GoodEventCriteria); err != nil {
			return snap, fmt.Errorf("unmarshal good_event_criteria: %w", err)
		}
	}

	_ = tx.Commit()
	return snap, nil
}

// List retrieves a paginated list of SLIs.
func (r *SLIReadRepository) List(ctx context.Context, tenantID, resourceGroupID string, limit, offset int) ([]sli.Snapshot, int64, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("begin sli list tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The group predicate belongs in the query, not in a filter over the page:
	// post-filtering would report a total that counts rows the caller may not
	// see.
	var total int64
	if err = tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM slis
			WHERE tenant_id = $1 AND deleted_at IS NULL
			  AND ($2::text IS NULL OR resource_group_id = $2)
		`, tenantID, nullableGroup(resourceGroupID)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count slis: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
			SELECT id, tenant_id, name, description, sli_type, source_type, source_config, aggregation, good_event_criteria, version, created_at, updated_at
			FROM slis
			WHERE tenant_id = $1 AND deleted_at IS NULL
			  AND ($2::text IS NULL OR resource_group_id = $2)
			ORDER BY id DESC
			LIMIT $3 OFFSET $4
		`, tenantID, nullableGroup(resourceGroupID), limit, offset)
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
			if err := json.Unmarshal(sourceConfigRaw, &snap.SourceConfig); err != nil {
				return nil, 0, fmt.Errorf("unmarshal source_config: %w", err)
			}
		}
		if len(goodEventRaw) > 0 {
			if err := json.Unmarshal(goodEventRaw, &snap.GoodEventCriteria); err != nil {
				return nil, 0, fmt.Errorf("unmarshal good_event_criteria: %w", err)
			}
		}
		slis = append(slis, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate slis: %w", err)
	}

	_ = tx.Commit()
	return slis, total, nil
}

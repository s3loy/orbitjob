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
			SELECT slis.id, slis.tenant_id, name, description, sli_type, source_type, source_config, aggregation, good_event_criteria, version, created_at, updated_at, v.configuration_version, COALESCE(o.statistics_semantics,'event_time_v2'), v.effective_from, o.last_event_at, o.last_sample_at, o.last_observed_at
			FROM slis LEFT JOIN sli_configuration_versions v ON v.tenant_id=slis.tenant_id AND v.sli_id=slis.id AND v.configuration_version=1 LEFT JOIN sli_observation_state o ON o.tenant_id=slis.tenant_id AND o.sli_id=slis.id
			WHERE slis.tenant_id = $1 AND id = $2 AND deleted_at IS NULL
			  AND ($3::text IS NULL OR resource_group_id = $3)
		`, tenantID, id, nullableGroup(resourceGroupID)).Scan(
		&snap.ID, &snap.TenantID, &snap.Name, &snap.Description, &snap.SLIType, &snap.SourceType,
		&sourceConfigRaw, &snap.Aggregation, &goodEventRaw, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt, &snap.ConfigurationVersion, &snap.StatisticsSemantics, &snap.ActiveFrom, &snap.LastEventAt, &snap.LastSampleAt, &snap.LastObservedAt,
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
	return r.ListSearch(ctx, tenantID, resourceGroupID, "", limit, offset)
}

// ListSearch applies literal search within the authorized scope before count
// and pagination, preserving the unfiltered List contract for existing callers.
func (r *SLIReadRepository) ListSearch(ctx context.Context, tenantID, resourceGroupID, search string, limit, offset int) ([]sli.Snapshot, int64, error) {
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
			WHERE slis.tenant_id = $1 AND deleted_at IS NULL
			  AND ($2::text IS NULL OR resource_group_id = $2)
			  AND ($3 = '' OR strpos(lower(name),lower($3)) > 0 OR strpos(lower(COALESCE(description,'')),lower($3)) > 0)
		`, tenantID, nullableGroup(resourceGroupID), search).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count slis: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
			SELECT slis.id, slis.tenant_id, name, description, sli_type, source_type, source_config, aggregation, good_event_criteria, version, created_at, updated_at, v.configuration_version, COALESCE(o.statistics_semantics,'event_time_v2'), v.effective_from, o.last_event_at, o.last_sample_at, o.last_observed_at
			FROM slis LEFT JOIN sli_configuration_versions v ON v.tenant_id=slis.tenant_id AND v.sli_id=slis.id AND v.configuration_version=1 LEFT JOIN sli_observation_state o ON o.tenant_id=slis.tenant_id AND o.sli_id=slis.id
			WHERE slis.tenant_id = $1 AND deleted_at IS NULL
			  AND ($2::text IS NULL OR resource_group_id = $2)
			  AND ($5 = '' OR strpos(lower(name),lower($5)) > 0 OR strpos(lower(COALESCE(description,'')),lower($5)) > 0)
			ORDER BY id DESC
			LIMIT $3 OFFSET $4
		`, tenantID, nullableGroup(resourceGroupID), limit, offset, search)
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
			&sourceConfigRaw, &snap.Aggregation, &goodEventRaw, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt, &snap.ConfigurationVersion, &snap.StatisticsSemantics, &snap.ActiveFrom, &snap.LastEventAt, &snap.LastSampleAt, &snap.LastObservedAt,
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

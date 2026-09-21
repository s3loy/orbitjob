package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	checkquery "orbitjob/internal/admin/app/check/query"
	domaincheck "orbitjob/internal/core/domain/check"
	"orbitjob/internal/domain/resource"
)

type CheckRepository struct {
	db *sql.DB
}

func NewCheckRepository(db *sql.DB) *CheckRepository {
	return &CheckRepository{db: db}
}

func (r *CheckRepository) Get(ctx context.Context, tenantID, resourceGroupID string, id int64) (domaincheck.Snapshot, error) {
	var snap domaincheck.Snapshot
	var checkConfigBytes, assertionBytes, labelsBytes []byte

	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return domaincheck.Snapshot{}, fmt.Errorf("begin check get tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx, `
			SELECT id, name, description, tenant_id, status, check_type, check_config, assertion_rules,
			       schedule_type, cron_expr, interval_sec, timezone, timeout_sec, retry_limit,
			       priority, labels, next_run_at, version, created_at, updated_at
			FROM checks
			WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
			  AND ($3::text IS NULL OR resource_group_id = $3)
		`, tenantID, id, nullableGroup(resourceGroupID)).Scan(
		&snap.ID, &snap.Name, &snap.Description, &snap.TenantID, &snap.Status, &snap.CheckType,
		&checkConfigBytes, &assertionBytes, &snap.ScheduleType, &snap.CronExpr, &snap.IntervalSec,
		&snap.Timezone, &snap.TimeoutSec, &snap.RetryLimit, &snap.Priority, &labelsBytes,
		&snap.NextRunAt, &snap.Version, &snap.CreatedAt, &snap.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return domaincheck.Snapshot{}, &resource.NotFoundError{
			Resource: "check",
			ID:       id,
		}
	}
	if err != nil {
		return domaincheck.Snapshot{}, fmt.Errorf("get check: %w", err)
	}

	if checkConfigBytes != nil {
		if err := json.Unmarshal(checkConfigBytes, &snap.CheckConfig); err != nil {
			return domaincheck.Snapshot{}, fmt.Errorf("unmarshal check_config: %w", err)
		}
	}
	if assertionBytes != nil {
		if err := json.Unmarshal(assertionBytes, &snap.AssertionRules); err != nil {
			return domaincheck.Snapshot{}, fmt.Errorf("unmarshal assertion_rules: %w", err)
		}
	}
	if labelsBytes != nil {
		if err := json.Unmarshal(labelsBytes, &snap.Labels); err != nil {
			return domaincheck.Snapshot{}, fmt.Errorf("unmarshal labels: %w", err)
		}
	}

	_ = tx.Commit()
	return snap, nil
}

func (r *CheckRepository) List(ctx context.Context, in checkquery.ListChecksInput) ([]checkquery.ListItem, int, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	tx, err := WithTenant(ctx, r.db, in.TenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("begin check list tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var total int
	// The group predicate is part of the query, not a filter over the fetched
	// page. Post-filtering would return the wrong total and would leak the
	// existence of other groups' checks through the count.
	err = tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM checks
			WHERE tenant_id = $1 AND deleted_at IS NULL
			  AND ($2::text IS NULL OR status = $2)
			  AND ($3::text IS NULL OR resource_group_id = $3)
		`, in.TenantID, in.Status, nullableGroup(in.ResourceGroupID)).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count checks: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
			SELECT id, name, description, tenant_id, status, check_type, schedule_type, next_run_at, version, created_at
			FROM checks
			WHERE tenant_id = $1 AND deleted_at IS NULL
			  AND ($2::text IS NULL OR status = $2)
			  AND ($3::text IS NULL OR resource_group_id = $3)
			ORDER BY id DESC
			LIMIT $4 OFFSET $5
		`, in.TenantID, in.Status, nullableGroup(in.ResourceGroupID), limit, in.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list checks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []checkquery.ListItem
	for rows.Next() {
		var item checkquery.ListItem
		var description sql.NullString
		var nextRunAt sql.NullTime
		err := rows.Scan(
			&item.ID, &item.Name, &description, &item.TenantID, &item.Status,
			&item.CheckType, &item.ScheduleType, &nextRunAt, &item.Version, &item.CreatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan check: %w", err)
		}
		if description.Valid {
			item.Description = &description.String
		}
		if nextRunAt.Valid {
			item.NextRunAt = &nextRunAt.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate checks: %w", err)
	}

	_ = tx.Commit()
	return items, total, nil
}

package postgres

import (
	"context"
	"fmt"

	query "orbitjob/internal/admin/app/job/query"
)

// List returns one page of active job definitions for a tenant, newest first.
//
// It is one round trip over job_definition_revisions. The partial unique index
// ux_job_definition_revisions_active guarantees at most one active revision per
// definition, so this lists each job exactly once.
func (r *JobRepository) List(ctx context.Context, in query.ListInput) ([]query.ListItem, error) {
	tx, err := WithTenant(ctx, r.db, in.TenantID)
	if err != nil {
		return nil, fmt.Errorf("begin job list tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, tenant_id, source_namespace, source_name, source_uid,
		       generation, actor, normalized_spec::text, created_at
		FROM job_definition_revisions
		WHERE tenant_id = $1 AND is_active
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, in.TenantID, in.Limit, in.Offset)
	if err != nil {
		return nil, fmt.Errorf("query job definitions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]query.ListItem, 0)
	for rows.Next() {
		item, err := scanDefinitionListItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate job definitions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit job list tx: %w", err)
	}
	return out, nil
}

// rowScanner is the shape shared by *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanDefinitionListItem(scanner rowScanner) (query.ListItem, error) {
	var (
		out query.ListItem
		raw []byte
	)
	if err := scanner.Scan(
		&out.ID,
		&out.TenantID,
		&out.Namespace,
		&out.Name,
		&out.SourceUID,
		&out.Generation,
		&out.Actor,
		&raw,
		&out.CreatedAt,
	); err != nil {
		return query.ListItem{}, fmt.Errorf("scan job definition: %w", err)
	}

	spec, err := decodeScheduledJobSpec(raw)
	if err != nil {
		return query.ListItem{}, err
	}
	out.Schedule = spec.Schedule
	out.Suspend = spec.Suspend
	out.ConcurrencyPolicy = string(spec.ConcurrencyPolicy)
	out.MisfirePolicy = string(spec.MisfirePolicy)
	out.ScheduleSummary = query.BuildScheduleSummary(out.Schedule, out.Suspend)
	return out, nil
}

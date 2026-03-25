package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	query "orbitjob/internal/admin/app/job/query"
)

// List queries control-plane job list items.
func (r *JobRepository) List(ctx context.Context, in query.ListInput) (_ []query.ListItem, err error) {
	const baseQuery = `
                SELECT
                    id,
                    name,
                    tenant_id,
                    trigger_type,
                    cron_expr,
                    timezone,
                    handler_type,
                    concurrency_policy,
                    misfire_policy,
                    status,
                    next_run_at,
                    last_scheduled_at,
                    created_at,
                    updated_at
                FROM jobs
                WHERE tenant_id = $1
                  AND deleted_at IS NULL
        `

	var rows *sql.Rows

	if in.Status == "" {
		rows, err = r.db.QueryContext(ctx, baseQuery+`
                        ORDER BY id DESC
                        LIMIT $2 OFFSET $3
                `, in.TenantID, in.Limit, in.Offset)
	} else {
		rows, err = r.db.QueryContext(ctx, baseQuery+`
                        AND status = $2
                        ORDER BY id DESC
                        LIMIT $3 OFFSET $4
                `, in.TenantID, in.Status, in.Limit, in.Offset)
	}
	if err != nil {
		slog.Error("job list failed",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("query job list: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close job list rows: %w", closeErr)
		}
	}()

	var out []query.ListItem
	for rows.Next() {
		item, err := scanJobListItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate job list: %w", err)
	}

	return out, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJobListItem(scanner rowScanner) (query.ListItem, error) {
	var out query.ListItem
	var cronExpr sql.NullString
	var timezone string
	var nextRunAt sql.NullTime
	var lastScheduledAt sql.NullTime

	err := scanner.Scan(
		&out.ID,
		&out.Name,
		&out.TenantID,
		&out.TriggerType,
		&cronExpr,
		&timezone,
		&out.HandlerType,
		&out.ConcurrencyPolicy,
		&out.MisfirePolicy,
		&out.Status,
		&nextRunAt,
		&lastScheduledAt,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return query.ListItem{}, fmt.Errorf("scan job list item: %w", err)
	}

	out.NextRunAt = nullTimePtr(nextRunAt)
	out.LastScheduledAt = nullTimePtr(lastScheduledAt)
	out.ScheduleSummary = query.BuildScheduleSummary(
		out.TriggerType,
		nullStringPtr(cronExpr),
		timezone,
	)

	return out, nil
}

func nullTimePtr(in sql.NullTime) *time.Time {
	if !in.Valid {
		return nil
	}

	t := in.Time
	return &t
}

func nullStringPtr(in sql.NullString) *string {
	if !in.Valid {
		return nil
	}

	s := in.String
	return &s
}

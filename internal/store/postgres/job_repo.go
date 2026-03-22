package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"orbitjob/internal/job"
)

type JobRepository struct {
	db *sql.DB
}

func NewJobRepository(db *sql.DB) *JobRepository {
	return &JobRepository{db: db}
}

// Create inserts a new job row and returns the persisted snapshot.
func (r *JobRepository) Create(ctx context.Context, in job.CreateJobSpec) (job.Job, error) {
	payload := in.HandlerPayload
	if payload == nil {
		payload = map[string]any{}
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return job.Job{}, fmt.Errorf("marshal handler_payload: %w", err)
	}

	var out job.Job
	var nextRunAt sql.NullTime

	err = r.db.QueryRowContext(ctx, `
				INSERT INTO jobs (
						name,
						tenant_id,
						trigger_type,
						cron_expr,
						timezone,
						handler_type,
						handler_payload,
						timeout_sec,
						retry_limit,
						retry_backoff_sec,
						retry_backoff_strategy,
						concurrency_policy,
						misfire_policy,
						next_run_at
                )
                VALUES (
						$1, $2, $3, $4, $5, $6, $7::jsonb,
						$8, $9, $10, $11, $12, $13, $14
				)
                RETURNING id, name, tenant_id, status, next_run_at, created_at, updated_at
        `,
		in.Name,
		in.TenantID,
		in.TriggerType,
		in.CronExpr,
		in.Timezone,
		in.HandlerType,
		string(payloadBytes),
		in.TimeoutSec,
		in.RetryLimit,
		in.RetryBackoffSec,
		in.RetryBackoffStrategy,
		in.ConcurrencyPolicy,
		in.MisfirePolicy,
		in.NextRunAt,
	).Scan(
		&out.ID,
		&out.Name,
		&out.TenantID,
		&out.Status,
		&nextRunAt,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return job.Job{}, fmt.Errorf("insert job: %w", err)
	}

	if nextRunAt.Valid {
		t := nextRunAt.Time
		out.NextRunAt = &t
	}

	return out, nil
}

// List queries control-plane job list items.
func (r *JobRepository) List(ctx context.Context, in job.ListJobsQuery) ([]job.JobListItem, error) {
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

	var (
		rows *sql.Rows
		err  error
	)

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
		return nil, fmt.Errorf("query job list: %w", err)
	}
	defer rows.Close()

	var out []job.JobListItem
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

func scanJobListItem(scanner rowScanner) (job.JobListItem, error) {
	var out job.JobListItem
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
		return job.JobListItem{}, fmt.Errorf("scan job list item: %w", err)
	}

	out.NextRunAt = nullTimePtr(nextRunAt)
	out.LastScheduledAt = nullTimePtr(lastScheduledAt)
	out.ScheduleSummary = job.BuildJobScheduleSummary(
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

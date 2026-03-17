package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"orbitjob/internal/job"
)

type JobRepository struct {
	db *sql.DB
}

func NewJobRepository(db *sql.DB) *JobRepository {
	return &JobRepository{db: db}
}

// Create inserts a new job row and returns the persisted snapshot.
func (r *JobRepository) Create(ctx context.Context, in job.CreateJobRequest) (job.Job, error) {
	tenantID := in.TenantID
	if tenantID == "" {
		tenantID = "default"
	}
	timezone := in.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

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
                        name, tenant_id, trigger_type, cron_expr, timezone,
                        handler_type, handler_payload
                )
                VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)
                RETURNING id, name, tenant_id, status, next_run_at, created_at, updated_at
        `,
		in.Name, tenantID, in.TriggerType, in.CronExpr, timezone,
		in.HandlerType, string(payloadBytes),
	).Scan(
		&out.ID, &out.Name, &out.TenantID, &out.Status, &out.NextRunAt, &out.CreatedAt, &out.UpdatedAt,
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

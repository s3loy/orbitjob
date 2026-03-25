package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	domainjob "orbitjob/internal/core/domain/job"
)

// Create inserts a new job row and returns the persisted snapshot.
func (r *JobRepository) Create(ctx context.Context, in domainjob.CreateSpec) (domainjob.Snapshot, error) {
	payload := in.HandlerPayload
	if payload == nil {
		payload = map[string]any{}
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("marshal handler_payload: %w", err)
	}

	var out domainjob.Snapshot
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
		slog.Error("job create failed",
			"error", err.Error(),
			"tenant_id", in.TenantID,
		)
		return domainjob.Snapshot{}, fmt.Errorf("insert job: %w", err)
	}

	if nextRunAt.Valid {
		t := nextRunAt.Time
		out.NextRunAt = &t
	}

	return out, nil
}

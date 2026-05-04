package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	domainjob "orbitjob/internal/core/domain/job"
	tenant "orbitjob/internal/core/domain/tenant"
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
							priority,
							partition_key,
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
							$1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb,
							$10, $11, $12, $13, $14, $15, $16
					)
	                RETURNING id, name, tenant_id, status, version, next_run_at, created_at, updated_at
	        `,
		in.Name,
		in.TenantID,
		in.Priority,
		in.PartitionKey,
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
		&out.Version,
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

	diffBytes, err := json.Marshal(map[string]any{
		"name":         in.Name,
		"trigger_type": in.TriggerType,
		"handler_type": in.HandlerType,
	})
	if err != nil {
		slog.Error("marshal audit diff failed", "error", err.Error())
	} else if _, err = r.db.ExecContext(ctx, `
		INSERT INTO audit_events (tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
	`,
		in.TenantID,
		tenant.ActorTypeSystem,
		"system",
		tenant.EventTypeJobCreated,
		tenant.ResourceTypeJob,
		fmt.Sprintf("%d", out.ID),
		string(diffBytes),
	); err != nil {
		slog.Error("insert audit event failed",
			"error", err.Error(),
			"tenant_id", in.TenantID,
			"job_id", out.ID,
		)
	}

	return out, nil
}

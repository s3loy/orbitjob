package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	query "orbitjob/internal/admin/app/job/query"
	"orbitjob/internal/domain/resource"
)

// Get queries one control-plane job detail item.
func (r *JobRepository) Get(ctx context.Context, in query.GetInput) (query.GetItem, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT
			id,
			name,
			tenant_id,
			version,
			priority,
			trigger_type,
			partition_key,
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
			status,
			next_run_at,
			last_scheduled_at,
			created_at,
			updated_at
		FROM jobs
		WHERE tenant_id = $1
		  AND id = $2
		  AND deleted_at IS NULL
	`,
		in.TenantID,
		in.ID,
	)

	item, err := scanJobGetItem(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return query.GetItem{}, &resource.NotFoundError{
				Resource: "job",
				ID:       in.ID,
			}
		}

		slog.Error("job get failed",
			"error", err.Error(),
			"tenant_id", in.TenantID,
			"id", in.ID,
		)
		return query.GetItem{}, fmt.Errorf("query job detail: %w", err)
	}

	return item, nil
}

func scanJobGetItem(scanner rowScanner) (query.GetItem, error) {
	var out query.GetItem
	var partitionKey sql.NullString
	var cronExpr sql.NullString
	var nextRunAt sql.NullTime
	var lastScheduledAt sql.NullTime
	var payloadBytes []byte

	err := scanner.Scan(
		&out.ID,
		&out.Name,
		&out.TenantID,
		&out.Version,
		&out.Priority,
		&out.TriggerType,
		&partitionKey,
		&cronExpr,
		&out.Timezone,
		&out.HandlerType,
		&payloadBytes,
		&out.TimeoutSec,
		&out.RetryLimit,
		&out.RetryBackoffSec,
		&out.RetryBackoffStrategy,
		&out.ConcurrencyPolicy,
		&out.MisfirePolicy,
		&out.Status,
		&nextRunAt,
		&lastScheduledAt,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return query.GetItem{}, err
	}

	out.PartitionKey = nullStringPtr(partitionKey)
	out.CronExpr = nullStringPtr(cronExpr)
	out.NextRunAt = nullTimePtr(nextRunAt)
	out.LastScheduledAt = nullTimePtr(lastScheduledAt)
	out.ScheduleSummary = query.BuildScheduleSummary(out.TriggerType, out.CronExpr, out.Timezone)

	if len(payloadBytes) == 0 {
		out.HandlerPayload = map[string]any{}
		return out, nil
	}

	if err := json.Unmarshal(payloadBytes, &out.HandlerPayload); err != nil {
		return query.GetItem{}, fmt.Errorf("decode handler_payload: %w", err)
	}
	if out.HandlerPayload == nil {
		out.HandlerPayload = map[string]any{}
	}

	return out, nil
}

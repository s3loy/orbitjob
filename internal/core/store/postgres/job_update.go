package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/domain/resource"
)

// Update persists mutable job fields with optimistic concurrency and writes an audit row.
func (r *JobRepository) Update(
	ctx context.Context,
	in domainjob.UpdateSpec,
	changedBy string,
) (_ domainjob.Snapshot, err error) {
	payloadBytes, err := json.Marshal(normalizePayload(in.HandlerPayload))
	if err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("marshal handler_payload: %w", err)
	}

	diffBytes, err := json.Marshal(buildUpdateDiffPayload(in))
	if err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("marshal audit diff_payload: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("begin job update tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var out domainjob.Snapshot
	var nextRunAt sql.NullTime

	err = tx.QueryRowContext(ctx, `
		UPDATE jobs
		SET name = $3,
		    priority = $4,
		    partition_key = $5,
		    trigger_type = $6,
		    cron_expr = $7,
		    timezone = $8,
		    handler_type = $9,
		    handler_payload = $10::jsonb,
		    timeout_sec = $11,
		    retry_limit = $12,
		    retry_backoff_sec = $13,
		    retry_backoff_strategy = $14,
		    concurrency_policy = $15,
		    misfire_policy = $16,
		    next_run_at = $17,
		    version = version + 1
		WHERE tenant_id = $1
		  AND id = $2
		  AND version = $18
		  AND deleted_at IS NULL
		RETURNING id, name, tenant_id, status, version, next_run_at, created_at, updated_at
	`,
		in.TenantID,
		in.ID,
		in.Name,
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
		in.Version,
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
		if errors.Is(err, sql.ErrNoRows) {
			return domainjob.Snapshot{}, classifyJobWriteFailure(ctx, tx, in.TenantID, in.ID)
		}

		slog.Error("job update failed",
			"error", err.Error(),
			"tenant_id", in.TenantID,
			"id", in.ID,
		)
		return domainjob.Snapshot{}, fmt.Errorf("update job: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO job_change_audits (
			tenant_id,
			job_id,
			action,
			changed_by,
			diff_payload
		)
		VALUES ($1, $2, 'update', $3, $4::jsonb)
	`,
		in.TenantID,
		in.ID,
		changedBy,
		string(diffBytes),
	); err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("insert job audit: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("commit job update tx: %w", err)
	}

	if nextRunAt.Valid {
		t := nextRunAt.Time
		out.NextRunAt = &t
	}

	return out, nil
}

func classifyJobWriteFailure(ctx context.Context, tx *sql.Tx, tenantID string, id int64) error {
	var existingID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM jobs
		WHERE tenant_id = $1
		  AND id = $2
		  AND deleted_at IS NULL
	`,
		tenantID,
		id,
	).Scan(&existingID)
	if errors.Is(err, sql.ErrNoRows) {
		return &resource.NotFoundError{
			Resource: "job",
			ID:       id,
		}
	}
	if err != nil {
		return fmt.Errorf("classify job write failure: %w", err)
	}

	return &resource.ConflictError{
		Resource: "job",
		ID:       id,
		Field:    "version",
		Message:  "stale job version",
	}
}

func buildUpdateDiffPayload(in domainjob.UpdateSpec) map[string]any {
	return map[string]any{
		"from_version":           in.Version,
		"to_version":             in.Version + 1,
		"name":                   in.Name,
		"priority":               in.Priority,
		"partition_key":          in.PartitionKey,
		"trigger_type":           in.TriggerType,
		"cron_expr":              in.CronExpr,
		"timezone":               in.Timezone,
		"handler_type":           in.HandlerType,
		"handler_payload":        normalizePayload(in.HandlerPayload),
		"timeout_sec":            in.TimeoutSec,
		"retry_limit":            in.RetryLimit,
		"retry_backoff_sec":      in.RetryBackoffSec,
		"retry_backoff_strategy": in.RetryBackoffStrategy,
		"concurrency_policy":     in.ConcurrencyPolicy,
		"misfire_policy":         in.MisfirePolicy,
	}
}

func normalizePayload(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}

	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

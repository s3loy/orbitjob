package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	domaininstance "orbitjob/internal/core/domain/instance"
	tenant "orbitjob/internal/core/domain/tenant"
	"orbitjob/internal/platform/metrics"
)

func (r *InstanceRepository) Create(ctx context.Context, in domaininstance.CreateSpec) (domaininstance.Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("begin instance create tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Set tenant context for RLS
	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", in.TenantID); err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("set tenant context: %w", err)
	}

	row := tx.QueryRowContext(ctx, `
			INSERT INTO job_instances (
				tenant_id,
				job_id,
				trigger_source,
				scheduled_at,
				status,
				priority,
				effective_priority,
				partition_key,
				idempotency_key,
				idempotency_scope,
				routing_key,
				attempt,
				max_attempt,
				trace_id
			)
			VALUES (
				$1, $2, $3, $4, 'pending', $5, $5, $6, $7, $8, $9, 1, $10, $11
			)
			RETURNING
				id,
				run_id::text,
				tenant_id,
				job_id,
				trigger_source,
				status,
				priority,
				effective_priority,
				partition_key,
				idempotency_key,
				idempotency_scope,
				routing_key,
				worker_id,
				attempt,
				max_attempt,
				scheduled_at,
				started_at,
				finished_at,
				lease_expires_at,
				dispatched_at,
				retry_at,
				result_code,
				error_msg,
				trace_id,
				created_at,
				updated_at,
			version
		`,
		in.TenantID,
		in.JobID,
		in.TriggerSource,
		in.ScheduledAt,
		in.Priority,
		in.PartitionKey,
		in.IdempotencyKey,
		in.IdempotencyScope,
		in.RoutingKey,
		in.MaxAttempt,
		in.TraceID,
	)

	out, err := scanInstanceSnapshot(row)
	if err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("insert job instance: %w", err)
	}

	diffBytes, err := json.Marshal(map[string]any{
		"job_id":         in.JobID,
		"trigger_source": in.TriggerSource,
	})
	if err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("marshal audit diff: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
			INSERT INTO audit_events (tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		`,
		in.TenantID,
		tenant.ActorTypeSystem,
		"system",
		tenant.EventTypeInstanceCreated,
		tenant.ResourceTypeInstance,
		out.RunID,
		string(diffBytes),
	); err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("insert audit event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("commit instance create tx: %w", err)
	}

	metrics.InstancesTotal.WithLabelValues(in.TenantID, domaininstance.StatusPending).Inc()
	return out, nil
}

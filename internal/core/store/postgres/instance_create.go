package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	domaininstance "orbitjob/internal/core/domain/instance"
	tenant "orbitjob/internal/core/domain/tenant"
)

func (r *InstanceRepository) Create(ctx context.Context, in domaininstance.CreateSpec) (domaininstance.Snapshot, error) {
	row := r.db.QueryRowContext(ctx, `
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
				updated_at
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
	if _, err = r.db.ExecContext(ctx, `
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

	return out, nil
}

package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	domaininstance "orbitjob/internal/core/domain/instance"
	tenant "orbitjob/internal/core/domain/tenant"
)

func (r *InstanceRepository) Cancel(ctx context.Context, tenantID, runID string, version int) (domaininstance.Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("begin instance cancel tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Set tenant context for RLS and ensure row-level tenant isolation.
	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("set tenant context: %w", err)
	}

	var currentStatus string
	if err = tx.QueryRowContext(ctx, `
		SELECT status FROM job_instances
		WHERE tenant_id = $1 AND run_id = $2
		FOR UPDATE
	`, tenantID, runID).Scan(&currentStatus); err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("read instance for cancel: %w", err)
	}

	row := tx.QueryRowContext(ctx, `
		UPDATE job_instances
		SET status = 'canceled',
		    version = version + 1,
		    finished_at = now()
		WHERE tenant_id = $1
		  AND run_id = $2
		  AND version = $3
		  AND status IN ('pending', 'dispatched', 'running', 'retry_wait')
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
	`, tenantID, runID, version)

	out, scanErr := scanInstanceSnapshot(row)
	if scanErr != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("cancel instance: %w", scanErr)
	}

	diffBytes, err := json.Marshal(map[string]any{
		"from_status": currentStatus,
		"to_status":   domaininstance.StatusCanceled,
	})
	if err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("marshal cancel diff: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO audit_events (tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
	`,
		out.TenantID,
		tenant.ActorTypeAPIKey,
		"api_key",
		domaininstance.StatusCanceled,
		tenant.ResourceTypeInstance,
		runID,
		string(diffBytes),
	); err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("insert audit event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("commit instance cancel tx: %w", err)
	}

	return out, nil
}

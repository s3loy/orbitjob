package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"orbitjob/internal/core/app/execute"
	domaininstance "orbitjob/internal/core/domain/instance"
	tenant "orbitjob/internal/core/domain/tenant"
)

var ErrInstanceNotClaimed = errors.New("instance not claimed: row not found or status changed")

type ExecutorRepository struct {
	db *sql.DB
}

func NewExecutorRepository(db *sql.DB) *ExecutorRepository {
	return &ExecutorRepository{db: db}
}

func (r *ExecutorRepository) ClaimNextDispatched(
	ctx context.Context,
	tenantID, workerID string,
	limit int,
	leaseExpiresAt, now time.Time,
) ([]execute.AssignedTask, error) {
	// Set tenant context for RLS
	if _, err := r.db.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return nil, fmt.Errorf("set tenant context: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		WITH claimed AS (
			SELECT id FROM job_instances
			WHERE tenant_id = $1
			  AND status = 'dispatched'
			ORDER BY effective_priority DESC, scheduled_at ASC, id ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		),
		updated AS (
			UPDATE job_instances ji
			SET status = 'running',
			    worker_id = $3,
			    started_at = $4,
			    lease_expires_at = $5
			FROM claimed
			WHERE ji.id = claimed.id
			RETURNING ji.id, ji.run_id::text, ji.tenant_id, ji.job_id,
			          ji.priority, ji.effective_priority, ji.attempt,
			          ji.max_attempt, ji.trace_id, ji.scheduled_at,
			          ji.dispatched_at, ji.lease_expires_at
		)
		SELECT u.id, u.run_id, u.tenant_id, u.job_id,
		       j.handler_type, j.handler_payload,
		       j.timeout_sec, j.retry_backoff_sec,
		       j.retry_backoff_strategy,
		       u.priority, u.effective_priority,
		       u.attempt, u.max_attempt, u.trace_id,
		       u.scheduled_at, u.dispatched_at, u.lease_expires_at
		FROM updated u
		JOIN jobs j ON u.tenant_id = j.tenant_id AND u.job_id = j.id
	`, tenantID, limit, workerID, now, leaseExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("claim dispatched instances: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tasks []execute.AssignedTask
	for rows.Next() {
		task, err := scanAssignedTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed instances: %w", err)
	}
	return tasks, nil
}

func (r *ExecutorRepository) CompleteInstance(
	ctx context.Context,
	spec domaininstance.CompleteSpec,
) error {
	// Set tenant context for RLS
	if _, err := r.db.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", spec.TenantID); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE job_instances
		SET status = $1,
		    finished_at = $2,
		    result_code = $3,
		    error_msg = $4,
		    retry_at = $5
		WHERE tenant_id = $6
		  AND id = $7
		  AND worker_id = $8
		  AND status = 'running'
	`,
		spec.Status,
		spec.FinishedAt,
		spec.ResultCode,
		spec.ErrorMsg,
		spec.RetryAt,
		spec.TenantID,
		spec.InstanceID,
		spec.WorkerID,
	)
	if err != nil {
		return fmt.Errorf("complete instance: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("complete instance rows affected: %w", err)
	}
	if n == 0 {
		return ErrInstanceNotClaimed
	}

	eventType := tenant.EventTypeInstanceCompleted
	if spec.Status == "retry_wait" {
		eventType = tenant.EventTypeInstanceStatusChanged
	}

	diff := map[string]any{
		"from_status": "running",
		"to_status":   spec.Status,
	}
	if spec.ResultCode != nil {
		diff["result_code"] = *spec.ResultCode
	}
	diffBytes, err := json.Marshal(diff)
	if err != nil {
		return fmt.Errorf("marshal audit diff: %w", err)
	}
	if _, err = r.db.ExecContext(ctx, `
		INSERT INTO audit_events (tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
	`,
		spec.TenantID,
		tenant.ActorTypeSystem,
		"worker",
		eventType,
		tenant.ResourceTypeInstance,
		fmt.Sprintf("%d", spec.InstanceID),
		string(diffBytes),
	); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}

	return nil
}

func (r *ExecutorRepository) ExtendLease(
	ctx context.Context,
	tenantID string,
	instanceID int64,
	workerID string,
	newExpiry time.Time,
) error {
	// Set tenant context for RLS
	if _, err := r.db.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE job_instances
		SET lease_expires_at = $1
		WHERE tenant_id = $2
		  AND id = $3
		  AND worker_id = $4
		  AND status IN ('dispatched', 'running')
	`, newExpiry, tenantID, instanceID, workerID)
	if err != nil {
		return fmt.Errorf("extend lease: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("extend lease rows affected: %w", err)
	}
	if n == 0 {
		return ErrInstanceNotClaimed
	}
	return nil
}

func scanAssignedTask(scanner rowScanner) (execute.AssignedTask, error) {
	var task execute.AssignedTask
	var payloadBytes []byte
	var traceID sql.NullString
	var leaseExpiresAt sql.NullTime
	var dispatchedAt sql.NullTime

	err := scanner.Scan(
		&task.InstanceID,
		&task.RunID,
		&task.TenantID,
		&task.JobID,
		&task.HandlerType,
		&payloadBytes,
		&task.TimeoutSec,
		&task.RetryBackoffSec,
		&task.RetryBackoffStrategy,
		&task.Priority,
		&task.EffectivePriority,
		&task.Attempt,
		&task.MaxAttempt,
		&traceID,
		&task.ScheduledAt,
		&dispatchedAt,
		&leaseExpiresAt,
	)
	if err != nil {
		return execute.AssignedTask{}, fmt.Errorf("scan assigned task: %w", err)
	}

	task.TraceID = nullStringPtr(traceID)
	if leaseExpiresAt.Valid {
		task.LeaseExpiresAt = leaseExpiresAt.Time
	}
	if dispatchedAt.Valid {
		task.DispatchedAt = dispatchedAt.Time
	}

	if len(payloadBytes) > 0 {
		if err := json.Unmarshal(payloadBytes, &task.HandlerPayload); err != nil {
			return execute.AssignedTask{}, fmt.Errorf("unmarshal handler_payload: %w", err)
		}
	}
	if task.HandlerPayload == nil {
		task.HandlerPayload = make(map[string]any)
	}

	return task, nil
}

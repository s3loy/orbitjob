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
)

var ErrInstanceNotClaimed = errors.New("instance not claimed: row not found or status changed")

type ExecutorRepository struct {
	db *sql.DB
}

func NewExecutorRepository(db *sql.DB) *ExecutorRepository {
	return &ExecutorRepository{db: db}
}

func (r *ExecutorRepository) FetchAssigned(
	ctx context.Context,
	tenantID, workerID string,
	limit int,
) ([]execute.AssignedTask, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT ji.id,
		       ji.run_id::text,
		       ji.tenant_id,
		       ji.job_id,
		       j.handler_type,
		       j.handler_payload,
		       j.timeout_sec,
		       j.retry_backoff_sec,
		       j.retry_backoff_strategy,
		       ji.priority,
		       ji.attempt,
		       ji.max_attempt,
		       ji.trace_id,
		       ji.scheduled_at,
		       ji.lease_expires_at
		FROM job_instances ji
		JOIN jobs j ON ji.tenant_id = j.tenant_id AND ji.job_id = j.id
		WHERE ji.tenant_id = $1
		  AND ji.worker_id = $2
		  AND ji.status = 'dispatching'
		ORDER BY ji.priority DESC, ji.scheduled_at ASC
		LIMIT $3
	`, tenantID, workerID, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch assigned instances: %w", err)
	}
	defer rows.Close()

	var tasks []execute.AssignedTask
	for rows.Next() {
		task, err := scanAssignedTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate assigned instances: %w", err)
	}
	return tasks, nil
}

func (r *ExecutorRepository) StartInstance(
	ctx context.Context,
	spec domaininstance.StartSpec,
) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE job_instances
		SET status = 'running',
		    started_at = $1
		WHERE tenant_id = $2
		  AND id = $3
		  AND worker_id = $4
		  AND status = 'dispatching'
	`, spec.StartedAt, spec.TenantID, spec.InstanceID, spec.WorkerID)
	if err != nil {
		return fmt.Errorf("start instance: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("start instance rows affected: %w", err)
	}
	if n == 0 {
		return ErrInstanceNotClaimed
	}
	return nil
}

func (r *ExecutorRepository) CompleteInstance(
	ctx context.Context,
	spec domaininstance.CompleteSpec,
) error {
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
	return nil
}

func (r *ExecutorRepository) ExtendLease(
	ctx context.Context,
	tenantID string,
	instanceID int64,
	workerID string,
	newExpiry time.Time,
) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE job_instances
		SET lease_expires_at = $1
		WHERE tenant_id = $2
		  AND id = $3
		  AND worker_id = $4
		  AND status IN ('dispatching', 'running')
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
		&task.Attempt,
		&task.MaxAttempt,
		&traceID,
		&task.ScheduledAt,
		&leaseExpiresAt,
	)
	if err != nil {
		return execute.AssignedTask{}, fmt.Errorf("scan assigned task: %w", err)
	}

	task.TraceID = nullStringPtr(traceID)
	if leaseExpiresAt.Valid {
		task.LeaseExpiresAt = leaseExpiresAt.Time
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

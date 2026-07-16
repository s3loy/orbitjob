package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"orbitjob/internal/platform/scan"

	domaininstance "orbitjob/internal/core/domain/instance"
)

type InstanceRepository struct {
	db *sql.DB
}

func NewInstanceRepository(db *sql.DB) *InstanceRepository {
	return &InstanceRepository{db: db}
}

func (r *InstanceRepository) GetByIdempotencyKey(
	ctx context.Context,
	tenantID, scope, key string,
) (domaininstance.Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("begin get instance by idempotency key tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Set tenant context for RLS
	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("set tenant context: %w", err)
	}

	row := tx.QueryRowContext(ctx, `
		SELECT
			id, run_id::text, tenant_id, job_id, trigger_source, status,
			priority, effective_priority, partition_key, idempotency_key,
			idempotency_scope, routing_key, worker_id, attempt, max_attempt,
			scheduled_at, started_at, finished_at, lease_expires_at,
			dispatched_at, retry_at, result_code, error_msg, trace_id,
			created_at, updated_at, version
		FROM job_instances
		WHERE tenant_id = $1
		  AND idempotency_scope = $2
		  AND idempotency_key = $3
	`, tenantID, scope, key)

	out, err := scanInstanceSnapshot(row)
	if err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("get instance by idempotency key: %w", err)
	}
	_ = tx.Commit()
	return out, nil
}

func scanInstanceSnapshot(scanner rowScanner) (domaininstance.Snapshot, error) {
	var out domaininstance.Snapshot
	var partitionKey sql.NullString
	var idempotencyKey sql.NullString
	var routingKey sql.NullString
	var workerID sql.NullString
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	var leaseExpiresAt sql.NullTime
	var dispatchedAt sql.NullTime
	var retryAt sql.NullTime
	var resultCode sql.NullString
	var errorMsg sql.NullString
	var traceID sql.NullString

	err := scanner.Scan(
		&out.ID,
		&out.RunID,
		&out.TenantID,
		&out.JobID,
		&out.TriggerSource,
		&out.Status,
		&out.Priority,
		&out.EffectivePriority,
		&partitionKey,
		&idempotencyKey,
		&out.IdempotencyScope,
		&routingKey,
		&workerID,
		&out.Attempt,
		&out.MaxAttempt,
		&out.ScheduledAt,
		&startedAt,
		&finishedAt,
		&leaseExpiresAt,
		&dispatchedAt,
		&retryAt,
		&resultCode,
		&errorMsg,
		&traceID,
		&out.CreatedAt,
		&out.UpdatedAt,
		&out.Version,
	)
	if err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("scan instance snapshot: %w", err)
	}

	out.PartitionKey = scan.NullStringPtr(partitionKey)
	out.IdempotencyKey = scan.NullStringPtr(idempotencyKey)
	out.RoutingKey = scan.NullStringPtr(routingKey)
	out.WorkerID = scan.NullStringPtr(workerID)
	out.StartedAt = scan.NullTimePtr(startedAt)
	out.FinishedAt = scan.NullTimePtr(finishedAt)
	out.LeaseExpiresAt = scan.NullTimePtr(leaseExpiresAt)
	out.DispatchedAt = scan.NullTimePtr(dispatchedAt)
	out.RetryAt = scan.NullTimePtr(retryAt)
	out.ResultCode = scan.NullStringPtr(resultCode)
	out.ErrorMsg = scan.NullStringPtr(errorMsg)
	out.TraceID = scan.NullStringPtr(traceID)

	return out, nil
}

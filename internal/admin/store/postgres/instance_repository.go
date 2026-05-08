package postgres

import (
	"context"
	"database/sql"
	"fmt"

	domaininstance "orbitjob/internal/core/domain/instance"
)

type InstanceRepository struct {
	db *sql.DB
}

func NewInstanceRepository(db *sql.DB) *InstanceRepository {
	return &InstanceRepository{db: db}
}

func (r *InstanceRepository) List(ctx context.Context, tenantID, status string, limit, offset int) ([]domaininstance.Snapshot, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
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
		FROM job_instances
		WHERE tenant_id = $1
		  AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`, tenantID, status, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domaininstance.Snapshot
	for rows.Next() {
		snap, err := scanInstanceSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("scan instance row: %w", err)
		}
		out = append(out, snap)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate instance rows: %w", err)
	}
	if out == nil {
		out = []domaininstance.Snapshot{}
	}
	return out, nil
}

func (r *InstanceRepository) GetByRunID(ctx context.Context, runID string) (domaininstance.Snapshot, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT
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
		FROM job_instances
		WHERE run_id = $1
	`, runID)

	return scanInstanceSnapshot(row)
}

func scanInstanceSnapshot(scanner interface {
	Scan(dest ...any) error
}) (domaininstance.Snapshot, error) {
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

	out.PartitionKey = nullStringPtr(partitionKey)
	out.IdempotencyKey = nullStringPtr(idempotencyKey)
	out.RoutingKey = nullStringPtr(routingKey)
	out.WorkerID = nullStringPtr(workerID)
	out.StartedAt = nullTimePtr(startedAt)
	out.FinishedAt = nullTimePtr(finishedAt)
	out.LeaseExpiresAt = nullTimePtr(leaseExpiresAt)
	out.DispatchedAt = nullTimePtr(dispatchedAt)
	out.RetryAt = nullTimePtr(retryAt)
	out.ResultCode = nullStringPtr(resultCode)
	out.ErrorMsg = nullStringPtr(errorMsg)
	out.TraceID = nullStringPtr(traceID)

	return out, nil
}

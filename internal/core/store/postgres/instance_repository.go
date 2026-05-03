package postgres

import (
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

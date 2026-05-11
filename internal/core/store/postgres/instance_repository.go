package postgres

import (
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

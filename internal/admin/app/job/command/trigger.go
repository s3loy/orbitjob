package command

import (
	"context"
	"fmt"
	"time"

	query "orbitjob/internal/admin/app/job/query"
	domaininstance "orbitjob/internal/core/domain/instance"
	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/metrics"
)

type jobReader interface {
	Get(ctx context.Context, in query.GetInput) (query.GetItem, error)
}

type instanceCreator interface {
	Create(ctx context.Context, in domaininstance.CreateSpec) (domaininstance.Snapshot, error)
}

type instanceReaderByIdempotency interface {
	GetByIdempotencyKey(ctx context.Context, tenantID, scope, key string) (domaininstance.Snapshot, error)
}

type TriggerJobUseCase struct {
	jobReader      jobReader
	instanceRepo   instanceCreator
	idempotency    instanceReaderByIdempotency
}

func NewTriggerJobUseCase(jobReader jobReader, instanceRepo instanceCreator, idempotency instanceReaderByIdempotency) *TriggerJobUseCase {
	return &TriggerJobUseCase{
		jobReader:    jobReader,
		instanceRepo: instanceRepo,
		idempotency:  idempotency,
	}
}

type TriggerInput struct {
	JobID    int64
	TenantID string
}

type TriggerResult struct {
	RunID       string    `json:"run_id"`
	JobID       int64     `json:"job_id"`
	TenantID    string    `json:"tenant_id"`
	Status      string    `json:"status"`
	ScheduledAt time.Time `json:"scheduled_at"`
	CreatedAt   time.Time `json:"created_at"`
	Created     bool      `json:"created"`
}

func (uc *TriggerJobUseCase) Trigger(ctx context.Context, in TriggerInput) (TriggerResult, error) {
	job, err := uc.jobReader.Get(ctx, query.GetInput{
		ID:       in.JobID,
		TenantID: in.TenantID,
	})
	if err != nil {
		return TriggerResult{}, fmt.Errorf("read job for trigger: %w", err)
	}
	if job.Status != domainjob.StatusActive {
		return TriggerResult{}, &resource.ConflictError{
			Resource: "job",
			Field:    "status",
			Message:  fmt.Sprintf("cannot trigger job with status %q", job.Status),
		}
	}
	spec, err := domaininstance.NormalizeCreate(domaininstance.CreateInput{
		TenantID:         in.TenantID,
		JobID:            in.JobID,
		TriggerSource:    domaininstance.TriggerSourceManual,
		ScheduledAt:      time.Now().UTC(),
		Priority:         job.Priority,
		PartitionKey:     job.PartitionKey,
		IdempotencyKey:   idempotencyKeyPtr(middleware.IdempotencyKey(ctx)),
		IdempotencyScope: domaininstance.DefaultIdempotencyScope,
		MaxAttempt:       job.RetryLimit + 1,
	})
	if err != nil {
		return TriggerResult{}, fmt.Errorf("normalize trigger instance: %w", err)
	}
	if spec.IdempotencyKey != nil && spec.IdempotencyScope != "" {
		existing, err := uc.idempotency.GetByIdempotencyKey(ctx, spec.TenantID, spec.IdempotencyScope, *spec.IdempotencyKey)
		if err == nil {
			return toTriggerResult(existing, false), nil
		}
	}

	start := time.Now()
	out, err := uc.instanceRepo.Create(ctx, spec)
	if err != nil {
		return TriggerResult{}, fmt.Errorf("create trigger instance: %w", err)
	}
	metrics.TriggerLatency.WithLabelValues(spec.TenantID).Observe(time.Since(start).Seconds())

	return toTriggerResult(out, true), nil
}

func toTriggerResult(snap domaininstance.Snapshot, created bool) TriggerResult {
	return TriggerResult{
		RunID:       snap.RunID,
		JobID:       snap.JobID,
		TenantID:    snap.TenantID,
		Status:      snap.Status,
		ScheduledAt: snap.ScheduledAt,
		CreatedAt:   snap.CreatedAt,
		Created:     created,
	}
}

func idempotencyKeyPtr(key string) *string {
	if key == "" {
		return nil
	}
	return &key
}

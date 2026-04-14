package command

import (
	"context"
	"time"

	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/platform/metrics"
)

type jobCreator interface {
	Create(ctx context.Context, in domainjob.CreateSpec) (domainjob.Snapshot, error)
}

type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type CreateJobUseCase struct {
	repo  jobCreator
	clock clock
}

func NewCreateJobUseCase(repo jobCreator) *CreateJobUseCase {
	return &CreateJobUseCase{
		repo:  repo,
		clock: realClock{},
	}
}

func (uc *CreateJobUseCase) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	spec, err := domainjob.NormalizeCreate(uc.clock.Now(), domainjob.CreateInput{
		Name:                 in.Name,
		TenantID:             in.TenantID,
		Priority:             in.Priority,
		PartitionKey:         in.PartitionKey,
		TriggerType:          in.TriggerType,
		CronExpr:             in.CronExpr,
		Timezone:             in.Timezone,
		HandlerType:          in.HandlerType,
		HandlerPayload:       in.HandlerPayload,
		TimeoutSec:           in.TimeoutSec,
		RetryLimit:           in.RetryLimit,
		RetryBackoffSec:      in.RetryBackoffSec,
		RetryBackoffStrategy: in.RetryBackoffStrategy,
		ConcurrencyPolicy:    in.ConcurrencyPolicy,
		MisfirePolicy:        in.MisfirePolicy,
	})
	if err != nil {
		return CreateResult{}, err
	}

	out, err := uc.repo.Create(ctx, spec)
	if err == nil {
		metrics.JobsTotal.WithLabelValues(spec.TenantID, spec.TriggerType).Inc()
	}

	return CreateResult{
		ID:        out.ID,
		Name:      out.Name,
		TenantID:  out.TenantID,
		Status:    out.Status,
		NextRunAt: out.NextRunAt,
		CreatedAt: out.CreatedAt,
		UpdatedAt: out.UpdatedAt,
	}, err
}

package command

import (
	"context"
	"time"

	domainjob "orbitjob/internal/domain/job"
	"orbitjob/internal/platform/metrics"
)

type jobCreator interface {
	Create(ctx context.Context, in domainjob.CreateSpec) (CreateResult, error)
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

func (uc *CreateJobUseCase) Create(ctx context.Context, in domainjob.CreateInput) (CreateResult, error) {
	spec, err := domainjob.NormalizeCreate(uc.clock.Now(), in)
	if err != nil {
		return CreateResult{}, err
	}

	out, err := uc.repo.Create(ctx, spec)
	if err == nil {
		metrics.JobsTotal.WithLabelValues(spec.TenantID, spec.TriggerType).Inc()
	}

	return out, err
}

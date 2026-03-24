package command

import (
	"context"
	"time"

	"orbitjob/internal/job"
	"orbitjob/internal/metrics"
)

type jobCreator interface {
	Create(ctx context.Context, in job.CreateJobSpec) (job.Job, error)
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

func (uc *CreateJobUseCase) Create(ctx context.Context, in job.CreateJobInput) (job.Job, error) {
	spec, err := job.NormalizeCreateJob(uc.clock.Now(), in)
	if err != nil {
		return job.Job{}, err
	}

	out, err := uc.repo.Create(ctx, spec)
	if err == nil {
		metrics.JobsTotal.WithLabelValues(spec.TenantID, string(spec.TriggerType)).Inc()
	}

	return out, err
}

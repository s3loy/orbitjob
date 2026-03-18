package jobapp

import (
	"context"
	"time"

	"orbitjob/internal/job"
)

type JobCreator interface {
	Create(ctx context.Context, in job.CreateJobSpec) (job.Job, error)
}

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type CreateJobUseCase struct {
	repo  JobCreator
	clock Clock
}

func NewCreateJobUseCase(repo JobCreator) *CreateJobUseCase {
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

	return uc.repo.Create(ctx, spec)
}

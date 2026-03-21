package jobapp

import (
	"context"

	"orbitjob/internal/job"
)

type jobListReader interface {
	List(ctx context.Context, in job.ListJobsQuery) ([]job.JobListItem, error)
}

type ListJobsUseCase struct {
	repo jobListReader
}

func NewListJobsUseCase(repo jobListReader) *ListJobsUseCase {
	return &ListJobsUseCase{
		repo: repo,
	}
}

func (uc *ListJobsUseCase) List(ctx context.Context, in job.ListJobsQuery) ([]job.JobListItem, error) {
	query, err := job.NormalizeListJobsQuery(in)
	if err != nil {
		return nil, err
	}

	return uc.repo.List(ctx, query)
}

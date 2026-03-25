package query

import (
	"context"
)

type jobListReader interface {
	List(ctx context.Context, in ListInput) ([]ListItem, error)
}

type ListJobsUseCase struct {
	repo jobListReader
}

func NewListJobsUseCase(repo jobListReader) *ListJobsUseCase {
	return &ListJobsUseCase{
		repo: repo,
	}
}

func (uc *ListJobsUseCase) List(ctx context.Context, in ListInput) ([]ListItem, error) {
	query, err := NormalizeListInput(in)
	if err != nil {
		return nil, err
	}

	return uc.repo.List(ctx, query)
}

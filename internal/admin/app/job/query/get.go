package query

import "context"

type jobGetReader interface {
	Get(ctx context.Context, in GetInput) (GetItem, error)
}

type GetJobUseCase struct {
	repo jobGetReader
}

func NewGetJobUseCase(repo jobGetReader) *GetJobUseCase {
	return &GetJobUseCase{
		repo: repo,
	}
}

func (uc *GetJobUseCase) Get(ctx context.Context, in GetInput) (GetItem, error) {
	query, err := NormalizeGetInput(in)
	if err != nil {
		return GetItem{}, err
	}

	return uc.repo.Get(ctx, query)
}

package command

import "context"

type DeleteInput struct {
	TenantID string
	// ResourceGroupID is the caller's own resource group scope, empty when the
	// caller is not scoped to one.
	ResourceGroupID string
	ID              int64
	Version         int
}

type checkDeleter interface {
	Delete(ctx context.Context, tenantID, resourceGroupID string, id int64, version int) error
}

type DeleteCheckUseCase struct {
	repo checkDeleter
}

func NewDeleteCheckUseCase(repo checkDeleter) *DeleteCheckUseCase {
	return &DeleteCheckUseCase{repo: repo}
}

func (uc *DeleteCheckUseCase) Delete(ctx context.Context, in DeleteInput) error {
	return uc.repo.Delete(ctx, in.TenantID, in.ResourceGroupID, in.ID, in.Version)
}

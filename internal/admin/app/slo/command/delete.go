package command

import (
	"context"

	"orbitjob/internal/domain/validation"
)

// sloDeleter soft-deletes SLOs.
type sloDeleter interface {
	Delete(ctx context.Context, tenantID string, id int64, version int) error
}

// DeleteSLOUseCase handles SLO deletion.
type DeleteSLOUseCase struct {
	repo sloDeleter
}

// NewDeleteSLOUseCase creates a new use case.
func NewDeleteSLOUseCase(repo sloDeleter) *DeleteSLOUseCase {
	return &DeleteSLOUseCase{repo: repo}
}

// DeleteInput contains the parameters for deletion.
type DeleteInput struct {
	TenantID string
	ID       int64
	Version  int
}

// Delete soft-deletes an SLO.
func (uc *DeleteSLOUseCase) Delete(ctx context.Context, in DeleteInput) error {
	if in.ID <= 0 {
		return validation.New("id", "id must be positive")
	}
	return uc.repo.Delete(ctx, in.TenantID, in.ID, in.Version)
}

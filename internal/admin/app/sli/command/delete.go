package command

import (
	"context"

	"orbitjob/internal/domain/validation"
)

// sliDeleter soft-deletes SLIs.
type sliDeleter interface {
	Delete(ctx context.Context, tenantID string, id int64, version int) error
}

// DeleteSLIUseCase handles SLI deletion.
type DeleteSLIUseCase struct {
	repo sliDeleter
}

// NewDeleteSLIUseCase creates a new use case.
func NewDeleteSLIUseCase(repo sliDeleter) *DeleteSLIUseCase {
	return &DeleteSLIUseCase{repo: repo}
}

// DeleteInput contains the parameters for deletion.
type DeleteInput struct {
	TenantID string
	ID       int64
	Version  int
}

// Delete soft-deletes an SLI.
func (uc *DeleteSLIUseCase) Delete(ctx context.Context, in DeleteInput) error {
	if in.ID <= 0 {
		return validation.New("id", "id must be positive")
	}
	return uc.repo.Delete(ctx, in.TenantID, in.ID, in.Version)
}

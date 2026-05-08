package command

import (
	"context"
	"time"

	domainjob "orbitjob/internal/core/domain/job"
)

type jobDeleter interface {
	Delete(ctx context.Context, tenantID string, id int64) (domainjob.Snapshot, error)
}

type DeleteJobUseCase struct {
	repo jobDeleter
}

func NewDeleteJobUseCase(repo jobDeleter) *DeleteJobUseCase {
	return &DeleteJobUseCase{repo: repo}
}

type DeleteInput struct {
	ID       int64
	TenantID string
}

type DeleteResult struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	TenantID  string    `json:"tenant_id"`
	Status    string    `json:"status"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (uc *DeleteJobUseCase) Delete(ctx context.Context, in DeleteInput) (DeleteResult, error) {
	out, err := uc.repo.Delete(ctx, in.TenantID, in.ID)
	if err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{
		ID:        out.ID,
		Name:      out.Name,
		TenantID:  out.TenantID,
		Status:    out.Status,
		Version:   out.Version,
		CreatedAt: out.CreatedAt,
		UpdatedAt: out.UpdatedAt,
	}, nil
}

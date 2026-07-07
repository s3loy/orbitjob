package query

import (
	"context"

	"orbitjob/internal/core/domain/tenant"
)

// GetInput is the control-plane query model for reading one tenant.
type GetInput struct {
	TenantID string
	ID       string
}

// TenantGetResult is the control-plane read model used by GET /api/v1/tenants/:id.
type TenantGetResult struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type tenantGetReader interface {
	Get(ctx context.Context, tenantID, id string) (tenant.Tenant, error)
}

// Getter reads one tenant.
type Getter struct {
	repo tenantGetReader
}

// NewGetter builds a Getter backed by repo.
func NewGetter(repo tenantGetReader) *Getter {
	return &Getter{repo: repo}
}

// Get returns one tenant by ID.
func (uc *Getter) Get(ctx context.Context, in GetInput) (TenantGetResult, error) {
	t, err := uc.repo.Get(ctx, in.TenantID, in.ID)
	if err != nil {
		return TenantGetResult{}, err
	}
	return TenantGetResult{
		ID:     t.ID,
		Slug:   t.Slug,
		Name:   t.Name,
		Status: t.Status,
	}, nil
}

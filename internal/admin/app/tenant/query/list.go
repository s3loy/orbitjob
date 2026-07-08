package query

import (
	"context"

	"orbitjob/internal/core/domain/tenant"
)

const (
	defaultListLimit = 50
	maxListLimit     = 100
)

// ListInput is the control-plane query model for listing tenants.
type ListInput struct {
	TenantID string
	Limit    int
	Offset   int
}

// TenantListItem is the control-plane read model used by GET /api/v1/tenants.
type TenantListItem struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type tenantListReader interface {
	List(ctx context.Context, tenantID string, limit, offset int) ([]tenant.Tenant, error)
}

// Lister lists tenants.
type Lister struct {
	repo tenantListReader
}

// NewLister builds a Lister backed by repo.
func NewLister(repo tenantListReader) *Lister {
	return &Lister{repo: repo}
}

// List returns a paginated list of tenants.
func (uc *Lister) List(ctx context.Context, in ListInput) ([]TenantListItem, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}
	tt, err := uc.repo.List(ctx, in.TenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]TenantListItem, len(tt))
	for i, t := range tt {
		out[i] = TenantListItem{
			ID:     t.ID,
			Slug:   t.Slug,
			Name:   t.Name,
			Status: t.Status,
		}
	}
	return out, nil
}

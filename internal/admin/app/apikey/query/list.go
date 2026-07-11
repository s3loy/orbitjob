package query

import (
	"context"
	"time"

	"orbitjob/internal/core/domain/apikey"
)

// ListInput is the control-plane query model for listing API keys.
type ListInput struct {
	TenantID string
}

// APIKeyListItem is the control-plane read model used by GET /api/v1/tenants/:id/api_keys.
type APIKeyListItem struct {
	ID        string     `json:"id"`
	KeyPrefix string     `json:"key_prefix"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type apiKeyListReader interface {
	ListByTenant(ctx context.Context, tenantID string) ([]apikey.APIKey, error)
}

// Lister lists API keys.
type Lister struct {
	repo apiKeyListReader
}

// NewLister builds a Lister backed by repo.
func NewLister(repo apiKeyListReader) *Lister {
	return &Lister{repo: repo}
}

// List returns API keys for a tenant.
func (uc *Lister) List(ctx context.Context, in ListInput) ([]APIKeyListItem, error) {
	items, err := uc.repo.ListByTenant(ctx, in.TenantID)
	if err != nil {
		return nil, err
	}

	out := make([]APIKeyListItem, len(items))
	for i, item := range items {
		out[i] = APIKeyListItem{
			ID:        item.ID,
			KeyPrefix: item.KeyPrefix,
			CreatedAt: item.CreatedAt,
			RevokedAt: item.RevokedAt,
		}
	}
	return out, nil
}

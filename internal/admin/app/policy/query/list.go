package query

import (
	"context"

	"orbitjob/internal/core/domain/policy"
)

// ListInput is the control-plane query model for listing policies.
type ListInput struct {
	TenantID string
}

// ListItem is one policy as the API exposes it. The document is included: a
// caller deciding which policy to bind needs to see what it grants, and the
// alternative -- a list of names to look up one by one -- is worse for the
// caller and no safer, since Get returns the same document.
type ListItem struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Platform    bool            `json:"platform"`
	Document    policy.Document `json:"document"`
}

type policyLister interface {
	List(ctx context.Context, tenantID string) ([]policy.Record, error)
}

// Lister reads the policies a tenant may bind.
type Lister struct{ policies policyLister }

func NewLister(policies policyLister) *Lister { return &Lister{policies: policies} }

// List returns the tenant's own policies and the platform presets, presets
// first. Each item is marked so a caller can tell which ones it may delete
// without learning that the hard way.
func (l *Lister) List(ctx context.Context, in ListInput) ([]ListItem, error) {
	records, err := l.policies.List(ctx, in.TenantID)
	if err != nil {
		return nil, err
	}
	items := make([]ListItem, 0, len(records))
	for _, rec := range records {
		items = append(items, toListItem(rec))
	}
	return items, nil
}

func toListItem(rec policy.Record) ListItem {
	return ListItem{
		ID:          rec.ID,
		Name:        rec.Name,
		Description: rec.Description,
		Platform:    rec.IsPlatformPreset(),
		Document:    rec.Document,
	}
}

package query

import (
	"context"

	"orbitjob/internal/core/domain/resourcegroup"
)

// ListInput is the control-plane query model for listing resource groups.
type ListInput struct {
	TenantID string
}

// ListItem is one group as the API exposes it.
type ListItem struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type groupLister interface {
	List(ctx context.Context, tenantID string) ([]resourcegroup.Group, error)
}

// Lister reads a tenant's resource groups.
type Lister struct{ groups groupLister }

func NewLister(groups groupLister) *Lister { return &Lister{groups: groups} }

// List returns the tenant's groups ordered by slug.
func (l *Lister) List(ctx context.Context, in ListInput) ([]ListItem, error) {
	groups, err := l.groups.List(ctx, in.TenantID)
	if err != nil {
		return nil, err
	}
	items := make([]ListItem, 0, len(groups))
	for _, g := range groups {
		items = append(items, ListItem{ID: g.ID, Slug: g.Slug, Name: g.Name})
	}
	return items, nil
}

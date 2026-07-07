package query

import "context"

const (
	defaultListLimit = 50
	maxListLimit     = 100
)

// ListInput is the control-plane query model for listing tenants.
type ListInput struct {
	Limit  int
	Offset int
}

// ListItem is the control-plane read model used by GET /api/v1/tenants.
type ListItem struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type tenantListReader interface {
	List(ctx context.Context, limit, offset int) ([]ListItem, error)
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
func (uc *Lister) List(ctx context.Context, in ListInput) ([]ListItem, error) {
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
	return uc.repo.List(ctx, limit, offset)
}

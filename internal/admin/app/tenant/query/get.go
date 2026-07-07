package query

import "context"

// GetInput is the control-plane query model for reading one tenant.
type GetInput struct {
	ID string
}

// GetResult is the control-plane read model used by GET /api/v1/tenants/:id.
type GetResult struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type tenantGetReader interface {
	Get(ctx context.Context, id string) (GetResult, error)
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
func (uc *Getter) Get(ctx context.Context, in GetInput) (GetResult, error) {
	return uc.repo.Get(ctx, in.ID)
}

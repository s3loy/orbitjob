package query

import (
	"context"

	"orbitjob/internal/core/domain/policy"
)

// GetInput is the control-plane query model for reading one policy.
type GetInput struct {
	TenantID string
	ID       string
}

// GetResult is one policy as the API exposes it.
type GetResult struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Platform    bool            `json:"platform"`
	Document    policy.Document `json:"document"`
}

type policyGetter interface {
	Get(ctx context.Context, tenantID, id string) (policy.Record, error)
}

// Getter reads one policy.
type Getter struct{ policies policyGetter }

func NewGetter(policies policyGetter) *Getter { return &Getter{policies: policies} }

// Get returns a policy when it is the tenant's own or a platform preset. The
// repository applies that scope; a policy belonging to another tenant is not
// found, which is the same answer as one that does not exist.
func (g *Getter) Get(ctx context.Context, in GetInput) (GetResult, error) {
	rec, err := g.policies.Get(ctx, in.TenantID, in.ID)
	if err != nil {
		return GetResult{}, err
	}
	return GetResult{
		ID:          rec.ID,
		Name:        rec.Name,
		Description: rec.Description,
		Platform:    rec.IsPlatformPreset(),
		Document:    rec.Document,
	}, nil
}

package command

import (
	"context"
	"fmt"

	"orbitjob/internal/core/domain/tenant"
	"orbitjob/internal/platform/scan"
)

// CreateInput is the control-plane command model for creating a tenant.
type CreateInput struct {
	Slug   string
	Name   string
	Status string
}

// CreateResult is the control-plane response model for a created tenant.
type CreateResult struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type tenantCreator interface {
	Create(ctx context.Context, t *tenant.Tenant) error
}

// Creator creates tenants.
type Creator struct {
	repo tenantCreator
}

// NewCreator builds a Creator backed by repo.
func NewCreator(repo tenantCreator) *Creator {
	return &Creator{repo: repo}
}

// Create normalizes input, generates an ID, and persists a new tenant.
func (c *Creator) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	normalized, err := tenant.NormalizeCreateTenant(tenant.CreateTenantInput{
		Slug: in.Slug,
		Name: in.Name,
	})
	if err != nil {
		return CreateResult{}, err
	}

	t := &tenant.Tenant{
		ID:     scan.GenerateID(),
		Slug:   normalized.Slug,
		Name:   normalized.Name,
		Status: in.Status,
	}
	if t.Status == "" {
		t.Status = tenant.StatusActive
	}
	if err := t.Validate(); err != nil {
		return CreateResult{}, err
	}

	if err := c.repo.Create(ctx, t); err != nil {
		return CreateResult{}, fmt.Errorf("create tenant: %w", err)
	}

	return CreateResult{
		ID:     t.ID,
		Slug:   t.Slug,
		Name:   t.Name,
		Status: t.Status,
	}, nil
}

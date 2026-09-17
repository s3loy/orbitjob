package command

import (
	"context"

	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/resourcegroup"
	"orbitjob/internal/platform/scan"
)

// CreateInput is the control-plane command model for creating a resource group.
type CreateInput struct {
	TenantID string
	Slug     string
	Name     string
	// ActorID is the key creating the group, recorded in the audit trail.
	ActorID string
}

// CreateResult is the control-plane response model for a created group.
type CreateResult struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// groupWriter stores the group and records its creation in one transaction.
type groupWriter interface {
	Create(ctx context.Context, g resourcegroup.Group, ev audit.Event) error
}

// Creator stores resource groups.
type Creator struct {
	groups groupWriter
}

func NewCreator(groups groupWriter) *Creator { return &Creator{groups: groups} }

// Create validates and stores a group.
func (c *Creator) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	g, err := resourcegroup.New(scan.GenerateID(), in.TenantID, in.Slug, in.Name)
	if err != nil {
		return CreateResult{}, err
	}
	if err := c.groups.Create(ctx, g, audit.Event{
		TenantID:     in.TenantID,
		ActorID:      in.ActorID,
		EventType:    audit.EventCreate,
		ResourceType: audit.ResourceGroup,
		ResourceID:   g.ID,
		Diff:         map[string]any{"slug": g.Slug, "name": g.Name},
	}); err != nil {
		return CreateResult{}, err
	}

	return CreateResult{ID: g.ID, Slug: g.Slug, Name: g.Name}, nil
}

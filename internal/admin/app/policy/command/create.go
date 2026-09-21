package command

import (
	"context"

	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/scan"
)

// CreateInput is the control-plane command model for creating a policy.
type CreateInput struct {
	TenantID    string
	Name        string
	Description string
	// Document is the policy body as submitted. It is carried raw so the bytes
	// validated are the bytes stored: decoding into a struct and re-encoding
	// would store a document nobody checked, and the two could drift.
	Document []byte
	// ActorID is the key creating the policy, recorded in the audit trail.
	ActorID string
}

// CreateResult is the control-plane response model for a created policy.
type CreateResult struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// policyWriter stores the policy and records who created it in one
// transaction, so a policy cannot exist without a trace of its author.
type policyWriter interface {
	Create(ctx context.Context, rec policy.Record, rawDocument []byte, ev audit.Event) error
}

// Creator stores tenant-authored policies.
type Creator struct {
	policies policyWriter
}

func NewCreator(policies policyWriter) *Creator {
	return &Creator{policies: policies}
}

// Create validates and stores a policy.
//
// Validation happens before the write, in two stages that catch different
// mistakes. ParseDocument rejects the shapes that make evaluation ambiguous --
// an unknown effect, an empty statement, a resource pattern that is not an
// ARN. ValidateTenantDocument then rejects any action the platform does not
// implement, plus the "*" wildcard. Neither is cosmetic: a tenant-authored
// policy naming an action nobody serves reads as a working grant, and the
// natural next move is to widen it until something happens.
func (c *Creator) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	doc, err := policy.ParseDocument(in.Document)
	if err != nil {
		return CreateResult{}, &validation.Error{Field: "document", Message: err.Error()}
	}
	if err := policy.ValidateTenantDocument(doc); err != nil {
		return CreateResult{}, &validation.Error{Field: "document", Message: err.Error()}
	}

	rec := policy.Record{
		ID:          scan.GenerateID(),
		TenantID:    in.TenantID,
		Name:        in.Name,
		Description: in.Description,
		Document:    doc,
	}
	event := audit.Event{
		TenantID:     in.TenantID,
		ActorID:      in.ActorID,
		EventType:    audit.EventCreate,
		ResourceType: audit.ResourcePolicy,
		ResourceID:   rec.ID,
		Diff:         map[string]any{"name": rec.Name, "document": doc},
	}
	if err := c.policies.Create(ctx, rec, in.Document, event); err != nil {
		return CreateResult{}, err
	}

	return CreateResult{ID: rec.ID, Name: rec.Name, Description: rec.Description}, nil
}

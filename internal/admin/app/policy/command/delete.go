package command

import (
	"context"

	"orbitjob/internal/core/domain/audit"
)

// DeleteInput is the control-plane command model for deleting a policy.
type DeleteInput struct {
	TenantID string
	ID       string
	// ActorID is the key deleting the policy, recorded in the audit trail.
	ActorID string
}

type policyDeleter interface {
	Delete(ctx context.Context, tenantID, id string, ev audit.Event) error
}

// Deleter removes tenant-owned policies.
type Deleter struct {
	policies policyDeleter
}

func NewDeleter(policies policyDeleter) *Deleter {
	return &Deleter{policies: policies}
}

// Delete removes a policy the tenant owns.
//
// Refusing to delete a platform preset is the repository's call, not this one:
// only a query can tell "not yours" from "not there", and guessing here would
// mean either leaking the existence of other tenants' policies or reporting a
// protected preset as missing.
func (d *Deleter) Delete(ctx context.Context, in DeleteInput) error {
	// A refused delete -- a platform preset, an unbound policy, a missing id --
	// returns before this, so no audit row claims a removal that did not happen.
	return d.policies.Delete(ctx, in.TenantID, in.ID, audit.Event{
		TenantID:     in.TenantID,
		ActorID:      in.ActorID,
		EventType:    audit.EventDelete,
		ResourceType: audit.ResourcePolicy,
		ResourceID:   in.ID,
	})
}

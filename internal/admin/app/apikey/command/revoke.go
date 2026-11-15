package command

import (
	"context"
	"errors"

	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/domain/resource"
)

// RevokeInput is the control-plane command model for revoking an API key.
type RevokeInput struct {
	ID       string
	TenantID string
	// ActorID is the key performing the revocation, recorded in the audit trail.
	ActorID string
}

type apiKeyRevoker interface {
	Revoke(ctx context.Context, tenantID, id string, ev audit.Event) error
	Scope(ctx context.Context, id string) (apikey.APIKey, error)
}

// Revoker revokes API keys.
type Revoker struct {
	repo apiKeyRevoker
}

// NewRevoker builds a Revoker backed by repo.
func NewRevoker(repo apiKeyRevoker) *Revoker {
	return &Revoker{repo: repo}
}

// Revoke marks an API key as revoked within a tenant, recording the revocation
// in the same transaction. A revocation that could not be recorded must not
// report success: afterwards, nothing could tell that it happened.
func (r *Revoker) Revoke(ctx context.Context, in RevokeInput) error {
	return r.repo.Revoke(ctx, in.TenantID, in.ID, audit.Event{
		TenantID:     in.TenantID,
		ActorID:      in.ActorID,
		EventType:    audit.EventRevoke,
		ResourceType: audit.ResourceAPIKey,
		ResourceID:   in.ID,
	})
}

// Target resolves the immutable owner/group identity the route guard authorizes.
func (r *Revoker) Target(ctx context.Context, id string) (apikey.APIKey, error) {
	return r.repo.Scope(ctx, id)
}

// APIKeyRevokeResult is the control-plane response model for a revoked API key.
type APIKeyRevokeResult struct {
	Revoked bool `json:"revoked"`
}

// IsNotFound reports whether err is an API key not-found error.
func IsNotFound(err error) bool {
	var nf *resource.NotFoundError
	return errors.As(err, &nf) && nf.Resource == "api_key"
}

package command

import (
	"context"
	"errors"

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
	RevokeCrossTenant(ctx context.Context, id string) error
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

// RevokeAsAdmin revokes an API key globally without tenant filtering.
// Only for use by platform administrators (bootstrap tenant).
//
// It still produces an audit row: the repository resolves the key's own tenant
// before revoking, so the event is recorded against the tenant it belongs to
// rather than left with no scope at all.
func (r *Revoker) RevokeAsAdmin(ctx context.Context, id string) error {
	return r.repo.RevokeCrossTenant(ctx, id)
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

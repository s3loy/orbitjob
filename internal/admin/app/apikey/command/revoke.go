package command

import (
	"context"
	"errors"

	"orbitjob/internal/domain/resource"
)

// RevokeInput is the control-plane command model for revoking an API key.
type RevokeInput struct {
	ID       string
	TenantID string
}

type apiKeyRevoker interface {
	Revoke(ctx context.Context, tenantID, id string) error
}

// Revoker revokes API keys.
type Revoker struct {
	repo apiKeyRevoker
}

// NewRevoker builds a Revoker backed by repo.
func NewRevoker(repo apiKeyRevoker) *Revoker {
	return &Revoker{repo: repo}
}

// Revoke marks an API key as revoked.
func (r *Revoker) Revoke(ctx context.Context, in RevokeInput) error {
	if err := r.repo.Revoke(ctx, in.TenantID, in.ID); err != nil {
		return err
	}
	return nil
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

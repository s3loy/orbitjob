package query

import (
	"strings"

	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// GetInput is the control-plane query model for reading one run detail.
type GetInput struct {
	TenantID string
	ID       int64
	// ResourceGroupID is the scope the caller's key is limited to; empty for an
	// unscoped caller. A run has no group, so a scoped caller is refused rather
	// than served the whole tenant's runs.
	ResourceGroupID string
}

// NormalizeGetInput trims and validates run detail query input.
func NormalizeGetInput(in GetInput) (GetInput, error) {
	if in.ID < 1 {
		return GetInput{}, validation.New("id", "must be >= 1")
	}

	if err := resource.RequireUnscoped(in.ResourceGroupID, "run"); err != nil {
		return GetInput{}, err
	}

	// Tenant ids are 26-character ULIDs (tenants.id); refusing here turns the
	// CHAR(26) column's write-time rejection into a 400. There is no default
	// tenant to fall back to.
	tenantID := strings.TrimSpace(in.TenantID)
	if len(tenantID) != 26 {
		return GetInput{}, validation.New("tenant_id", "must be a 26-character tenant id")
	}

	return GetInput{TenantID: tenantID, ID: in.ID, ResourceGroupID: in.ResourceGroupID}, nil
}

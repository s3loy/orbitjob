package query

import (
	"strings"

	"orbitjob/internal/domain/validation"
)

// GetInput is the control-plane query model for reading one job detail.
type GetInput struct {
	ID       int64
	TenantID string
}

// NormalizeGetInput trims and validates detail query input for control-plane reads.
func NormalizeGetInput(in GetInput) (GetInput, error) {
	if in.ID < 1 {
		return GetInput{}, validation.New("id", "must be >= 1")
	}

	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		tenantID = defaultTenantID
	}
	if len(tenantID) > 64 {
		return GetInput{}, validation.New("tenant_id", "must be <= 64 characters")
	}

	return GetInput{
		ID:       in.ID,
		TenantID: tenantID,
	}, nil
}

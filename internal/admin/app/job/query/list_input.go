package query

import (
	"strings"

	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

const (
	// DefaultListLimit is the page size used when the caller names none.
	DefaultListLimit = 50
	// MaxListLimit caps a page so one request cannot stream every definition.
	MaxListLimit = 100
)

// ListInput is the control-plane query model for listing job definitions.
//
// There is no resource group filter and no status filter: a definition has
// neither a group column nor a mutable status, and inventing either would
// describe a row the schema does not have.
type ListInput struct {
	TenantID string
	// ResourceGroupID is the scope the caller's key is limited to; empty for an
	// unscoped caller. A definition has no group, so a scoped caller is refused
	// rather than shown the whole tenant's definitions.
	ResourceGroupID string
	Limit           int
	Offset          int
}

// NormalizeListInput trims and validates list query input for control-plane reads.
func NormalizeListInput(in ListInput) (ListInput, error) {
	// Scope is decided first: a scoped caller is refused even when the rest of
	// the input is invalid, so an authorization denial never depends on the
	// request being otherwise well-formed. The get gate uses the same order.
	if err := resource.RequireUnscoped(in.ResourceGroupID, "job definition"); err != nil {
		return ListInput{}, err
	}

	// Tenant ids are 26-character ULIDs (tenants.id); refusing here turns the
	// CHAR(26) column's write-time rejection into a 400. There is no default
	// tenant to fall back to.
	tenantID := strings.TrimSpace(in.TenantID)
	if len(tenantID) != 26 {
		return ListInput{}, validation.New("tenant_id", "must be a 26-character tenant id")
	}

	limit := in.Limit
	if limit == 0 {
		limit = DefaultListLimit
	}
	if limit < 1 || limit > MaxListLimit {
		return ListInput{}, validation.Errorf("limit", "must be between 1 and %d", MaxListLimit)
	}

	offset := in.Offset
	if offset < 0 {
		return ListInput{}, validation.New("offset", "must be >= 0")
	}

	return ListInput{TenantID: tenantID, ResourceGroupID: in.ResourceGroupID, Limit: limit, Offset: offset}, nil
}

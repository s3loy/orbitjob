package query

import (
	"strings"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

const (
	// DefaultListLimit is the page size used when the caller names none.
	DefaultListLimit = 50
	// MaxListLimit caps a page so one request cannot stream the whole ledger.
	MaxListLimit = 100
)

// listablePhases is the set a caller may filter on. A phase that is not in this
// set is a typo, not an empty result, so it is rejected rather than passed to
// the query where it would silently match nothing.
var listablePhases = map[string]struct{}{
	string(jobrun.Pending):         {},
	string(jobrun.CreatingAttempt): {},
	string(jobrun.Running):         {},
	string(jobrun.RetryWaiting):    {},
	string(jobrun.Succeeded):       {},
	string(jobrun.Failed):          {},
	string(jobrun.CancelRequested): {},
	string(jobrun.Canceled):        {},
	string(jobrun.CancelUnknown):   {},
}

// ListInput is the control-plane query model for listing runs.
type ListInput struct {
	TenantID string
	// Phase narrows the list to one run lifecycle phase. Empty lists every phase.
	Phase string
	// ResourceGroupID is the scope the caller's key is limited to; empty for an
	// unscoped caller. A run has no group, so a scoped caller is refused rather
	// than shown the whole tenant's ledger.
	ResourceGroupID string
	Limit           int
	Offset          int
}

// NormalizeListInput trims and validates list query input for ledger reads.
func NormalizeListInput(in ListInput) (ListInput, error) {
	// Scope is decided first: a scoped caller is refused even when the rest of
	// the input is invalid, so an authorization denial never depends on the
	// request being otherwise well-formed. The get gate uses the same order.
	if err := resource.RequireUnscoped(in.ResourceGroupID, "run"); err != nil {
		return ListInput{}, err
	}

	// Tenant ids are 26-character ULIDs (tenants.id); refusing here turns the
	// CHAR(26) column's write-time rejection into a 400. There is no default
	// tenant to fall back to.
	tenantID := strings.TrimSpace(in.TenantID)
	if len(tenantID) != 26 {
		return ListInput{}, validation.New("tenant_id", "must be a 26-character tenant id")
	}

	phase := strings.TrimSpace(in.Phase)
	if phase != "" {
		if _, ok := listablePhases[phase]; !ok {
			return ListInput{}, validation.New("phase", "is not a known run phase")
		}
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

	return ListInput{
		TenantID:        tenantID,
		Phase:           phase,
		ResourceGroupID: in.ResourceGroupID,
		Limit:           limit,
		Offset:          offset,
	}, nil
}

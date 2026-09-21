package http

import (
	"strings"

	"orbitjob/internal/domain/validation"
)

// Shared input normalization for the function and workflow use cases. The
// checks they run are the read model's own: tenant ids are tenants.id values,
// pages are bounded, and a caller's group scope decides which rows it sees.

// normalizeTenantID validates the tenant a tenant-scoped use case acts on.
// Tenant ids are tenants.id values, 26-character ULIDs; the CHAR(26) columns
// and their foreign keys reject anything else at write time, and a 400 here is
// that rejection with its type intact.
func normalizeTenantID(tenantID string) (string, error) {
	tenantID = strings.TrimSpace(tenantID)
	if len(tenantID) != 26 {
		return "", validation.New("tenant_id", "must be a 26-character tenant id")
	}
	return tenantID, nil
}

// normalizeListLimit resolves the page size a list call uses. The binding
// layer caps what a client can send; this floor covers the value zero (no page
// named) and keeps a programmatically built input honest.
func normalizeListLimit(limit int) int {
	if limit < 1 {
		return defaultListPageLimit
	}
	if limit > maxListPageLimit {
		return maxListPageLimit
	}
	return limit
}

const (
	defaultListPageLimit = 50
	maxListPageLimit     = 100
)

// visibleToGroup decides whether one row is in a caller's scope. An unscoped
// caller (empty scope) sees everything; a scoped caller sees only rows stamped
// with its group, mirroring the checks read model's
// `resource_group_id = $caller_group` filter, where an ungrouped row is as
// invisible to a scoped key as another group's row.
func visibleToGroup(rowGroup, callerScope string) bool {
	return callerScope == "" || rowGroup == callerScope
}

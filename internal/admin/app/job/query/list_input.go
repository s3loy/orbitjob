package query

import (
	"strings"

	"orbitjob/internal/domain/validation"
)

const (
	DefaultListLimit = 50
	MaxListLimit     = 100

	StatusActive = "active"
	StatusPaused = "paused"

	defaultTenantID = "default"
)

// ListInput is the control-plane query model for listing jobs.
type ListInput struct {
	TenantID string
	Status   string
	Limit    int
	Offset   int
}

// NormalizeListInput trims and validates list query input for control-plane reads.
func NormalizeListInput(in ListInput) (ListInput, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		tenantID = defaultTenantID
	}
	if len(tenantID) > 64 {
		return ListInput{}, validation.New("tenant_id", "must be <= 64 characters")
	}

	status := strings.TrimSpace(in.Status)
	if status != "" && !isOneOf(status, StatusActive, StatusPaused) {
		return ListInput{}, validation.Errorf("status", "must be one of: %s, %s", StatusActive, StatusPaused)
	}

	limit := in.Limit
	if limit == 0 {
		limit = DefaultListLimit
	}
	if limit < 1 {
		return ListInput{}, validation.New("limit", "must be >= 1")
	}
	if limit > MaxListLimit {
		return ListInput{}, validation.Errorf("limit", "must be <= %d", MaxListLimit)
	}

	offset := in.Offset
	if offset < 0 {
		return ListInput{}, validation.New("offset", "must be >= 0")
	}

	return ListInput{
		TenantID: tenantID,
		Status:   status,
		Limit:    limit,
		Offset:   offset,
	}, nil
}

func isOneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

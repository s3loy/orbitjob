package job

import (
	"fmt"
	"strings"
)

const (
	DefaultListJobsLimit = 50
	MaxListJobsLimit     = 100

	JobStatusActive = "active"
	JobStatusPaused = "paused"
)

// ListJobsQuery is the internal query model for control-plane job listing.
type ListJobsQuery struct {
	TenantID string
	Status   string
	Limit    int
	Offset   int
}

func NormalizeListJobsQuery(in ListJobsQuery) (ListJobsQuery, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	if len(tenantID) > 64 {
		return ListJobsQuery{}, validationError("tenant_id", "must be <= 64 characters")
	}

	status := strings.TrimSpace(in.Status)
	if status != "" && !isOneOf(status, JobStatusActive, JobStatusPaused) {
		return ListJobsQuery{}, validationErrorf(
			"status",
			"must be one of: %s, %s",
			JobStatusActive,
			JobStatusPaused,
		)
	}

	limit := in.Limit
	if limit == 0 {
		limit = DefaultListJobsLimit
	}
	if limit < 1 {
		return ListJobsQuery{}, validationError("limit", "must be >= 1")
	}
	if limit > MaxListJobsLimit {
		return ListJobsQuery{}, validationErrorf("limit", "must be <= %d", MaxListJobsLimit)
	}

	offset := in.Offset
	if offset < 0 {
		return ListJobsQuery{}, validationError("offset", "must be >= 0")
	}

	return ListJobsQuery{
		TenantID: tenantID,
		Status:   status,
		Limit:    limit,
		Offset:   offset,
	}, nil
}

// BuildJobScheduleSummary builds the list-view schedule summary from stored job fields.
func BuildJobScheduleSummary(triggerType string, cronExpr *string, timezone string) string {
	switch triggerType {
	case TriggerTypeManual:
		return "manual"
	case TriggerTypeCron:
		expr := ""
		if cronExpr != nil {
			expr = strings.TrimSpace(*cronExpr)
		}

		tz := strings.TrimSpace(timezone)
		if tz == "" {
			tz = DefaultTimezone
		}

		if expr == "" {
			return fmt.Sprintf("cron (%s)", tz)
		}

		return fmt.Sprintf("cron: %s (%s)", expr, tz)
	default:
		return strings.TrimSpace(triggerType)
	}
}

package query

import (
	"strings"
	"time"
)

// ListItem is the control-plane read model used by GET /api/v1/jobs.
type ListItem struct {
	ID         int64  `json:"id"`
	TenantID   string `json:"tenant_id"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	SourceUID  string `json:"source_uid"`
	Generation int64  `json:"generation"`
	Actor      string `json:"actor"`

	Schedule          string `json:"schedule"`
	Suspend           bool   `json:"suspend"`
	ConcurrencyPolicy string `json:"concurrency_policy"`
	MisfirePolicy     string `json:"misfire_policy"`
	ScheduleSummary   string `json:"schedule_summary"`

	CreatedAt time.Time `json:"created_at"`
}

// BuildScheduleSummary renders the one-line schedule description for a
// definition. Suspend wins over the expression: a suspended definition does not
// fire, and saying "cron: ..." for one that is stopped answers the wrong
// question.
func BuildScheduleSummary(schedule string, suspend bool) string {
	if suspend {
		return "suspended"
	}
	expr := strings.TrimSpace(schedule)
	if expr == "" {
		return "unscheduled"
	}
	return "cron: " + expr
}

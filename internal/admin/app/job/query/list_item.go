package query

import (
	"fmt"
	"strings"
	"time"

	domainjob "orbitjob/internal/domain/job"
)

// ListItem is the control-plane read model used by GET /api/v1/jobs.
type ListItem struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	TenantID string `json:"tenant_id"`

	TriggerType       string `json:"trigger_type"`
	ScheduleSummary   string `json:"schedule_summary"`
	HandlerType       string `json:"handler_type"`
	ConcurrencyPolicy string `json:"concurrency_policy"`
	MisfirePolicy     string `json:"misfire_policy"`
	Status            string `json:"status"`

	NextRunAt       *time.Time `json:"next_run_at"`
	LastScheduledAt *time.Time `json:"last_scheduled_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// BuildScheduleSummary builds the list-view schedule summary from stored job fields.
func BuildScheduleSummary(triggerType string, cronExpr *string, timezone string) string {
	switch triggerType {
	case domainjob.TriggerTypeManual:
		return "manual"
	case domainjob.TriggerTypeCron:
		expr := ""
		if cronExpr != nil {
			expr = strings.TrimSpace(*cronExpr)
		}

		tz := strings.TrimSpace(timezone)
		if tz == "" {
			tz = domainjob.DefaultTimezone
		}

		if expr == "" {
			return fmt.Sprintf("cron (%s)", tz)
		}

		return fmt.Sprintf("cron: %s (%s)", expr, tz)
	default:
		return strings.TrimSpace(triggerType)
	}
}

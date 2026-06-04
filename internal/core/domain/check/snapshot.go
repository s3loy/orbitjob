package check

import "time"

// Snapshot is the persisted check state returned by the core write side.
type Snapshot struct {
	ID             int64          `json:"id"`
	Name           string         `json:"name"`
	Description    *string        `json:"description,omitempty"`
	TenantID       string         `json:"tenant_id"`
	Status         string         `json:"status"`
	CheckType      string         `json:"check_type"`
	CheckConfig    map[string]any `json:"check_config"`
	AssertionRules []AssertionRule `json:"assertion_rules"`
	ScheduleType   string         `json:"schedule_type"`
	CronExpr       *string        `json:"cron_expr,omitempty"`
	IntervalSec    *int           `json:"interval_sec,omitempty"`
	Timezone       string         `json:"timezone"`
	TimeoutSec     int            `json:"timeout_sec"`
	RetryLimit     int            `json:"retry_limit"`
	Priority       int            `json:"priority"`
	Labels         map[string]any `json:"labels"`
	NextRunAt      *time.Time     `json:"next_run_at,omitempty"`
	Version        int            `json:"version"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

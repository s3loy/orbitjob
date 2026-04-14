package query

import "time"

// GetItem is the control-plane read model used by GET /api/v1/jobs/:id.
type GetItem struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	TenantID string `json:"tenant_id"`
	Version  int    `json:"version"`
	Priority int    `json:"priority"`

	TriggerType          string         `json:"trigger_type"`
	PartitionKey         *string        `json:"partition_key"`
	CronExpr             *string        `json:"cron_expr"`
	Timezone             string         `json:"timezone"`
	ScheduleSummary      string         `json:"schedule_summary"`
	HandlerType          string         `json:"handler_type"`
	HandlerPayload       map[string]any `json:"handler_payload"`
	TimeoutSec           int            `json:"timeout_sec"`
	RetryLimit           int            `json:"retry_limit"`
	RetryBackoffSec      int            `json:"retry_backoff_sec"`
	RetryBackoffStrategy string         `json:"retry_backoff_strategy"`
	ConcurrencyPolicy    string         `json:"concurrency_policy"`
	MisfirePolicy        string         `json:"misfire_policy"`
	Status               string         `json:"status"`

	NextRunAt       *time.Time `json:"next_run_at"`
	LastScheduledAt *time.Time `json:"last_scheduled_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

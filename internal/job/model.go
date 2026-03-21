package job

import "time"

// Job is the read model returned to API callers
type Job struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`

	// NextRunAt is nullable. Manual jobs may not have a schedule timestamp.
	NextRunAt *time.Time `json:"next_run_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// JobListItem is the control-plane read model used by GET /api/v1/jobs.
type JobListItem struct {
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

package job

import "time"

// CreateJobRequest defines the input payload for creating a job definition.
type CreateJobRequest struct {
	Name                 string         `json:"name" binding:"required,max=128"`
	TenantID             string         `json:"tenant_id"`
	TriggerType          string         `json:"trigger_type" binding:"required,oneof=cron manual"`
	CronExpr             *string        `json:"cron_expr"`
	Timezone             string         `json:"timezone"`
	HandlerType          string         `json:"handler_type" binding:"required,max=32"`
	HandlerPayload       map[string]any `json:"handler_payload"`
	TimeoutSec           int            `json:"timeout_sec" binding:"omitempty,min=1"`
	RetryLimit           int            `json:"retry_limit" binding:"omitempty,min=0"`
	RetryBackoffSec      int            `json:"retry_backoff_sec" binding:"omitempty,min=0"`
	RetryBackoffStrategy string         `json:"retry_backoff_strategy" binding:"omitempty,oneof=fixed exponential"`
	ConcurrencyPolicy    string         `json:"concurrency_policy" binding:"omitempty,oneof=allow forbid replace"`
	MisfirePolicy        string         `json:"misfire_policy" binding:"omitempty,oneof=skip fire_now catch_up"`
}

// Job is the read model returned to API callers
// NextRunAt is nullable because manual jobs may not have schedule time.
type Job struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	TenantID  string     `json:"tenant_id"`
	Status    string     `json:"status"`
	NextRunAt *time.Time `json:"next_run_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

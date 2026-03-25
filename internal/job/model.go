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

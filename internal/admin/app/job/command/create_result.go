package command

import "time"

// CreateResult is the control-plane snapshot returned after creating a job.
type CreateResult struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`

	NextRunAt *time.Time `json:"next_run_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

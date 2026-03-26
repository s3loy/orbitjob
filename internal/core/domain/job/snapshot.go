package job

import "time"

// Snapshot is the persisted job state returned by the core write side.
type Snapshot struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`

	NextRunAt *time.Time `json:"next_run_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

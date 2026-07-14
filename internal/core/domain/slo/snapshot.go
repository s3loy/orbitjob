package slo

import "time"

// Snapshot is the persisted SLO state.
type Snapshot struct {
	ID                int64         `json:"id"`
	TenantID          string        `json:"tenant_id"`
	Name              string        `json:"name"`
	Description       *string       `json:"description,omitempty"`
	SLIID             int64         `json:"sli_id"`
	Target            float64       `json:"target"`
	WindowType        string        `json:"window_type"`
	WindowDuration    time.Duration `json:"window_duration"`
	AlertFastBurnRate float64       `json:"alert_fast_burn_rate"`
	AlertSlowBurnRate float64       `json:"alert_slow_burn_rate"`
	Status            string        `json:"status"`
	Version           int           `json:"version"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

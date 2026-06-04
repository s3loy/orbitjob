package checkrun

import "time"

const (
	StatusPending  = "pending"
	StatusRunning  = "running"
	StatusSuccess  = "success"
	StatusFailed   = "failed"

	SeverityOK       = "ok"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
	SeverityUnknown  = "unknown"
)

// Snapshot is the persisted check run state.
type Snapshot struct {
	ID                 int64          `json:"id"`
	RunID              string         `json:"run_id"`
	TenantID           string         `json:"tenant_id"`
	CheckID            int64          `json:"check_id"`
	Status             string         `json:"status"`
	Severity           *string        `json:"severity,omitempty"`
	Output             map[string]any `json:"output"`
	EvaluationResult   map[string]any `json:"evaluation_result"`
	ScheduledAt        time.Time      `json:"scheduled_at"`
	StartedAt          *time.Time     `json:"started_at,omitempty"`
	FinishedAt         *time.Time     `json:"finished_at,omitempty"`
	DurationMs         *int           `json:"duration_ms,omitempty"`
	Version            int            `json:"version"`
	CreatedAt          time.Time      `json:"created_at"`
}

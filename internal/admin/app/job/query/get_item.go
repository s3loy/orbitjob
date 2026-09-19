package query

import "time"

// GetItem is the control-plane read model used by GET /api/v1/jobs/:id.
//
// A job is no longer a row the API creates; it is a ScheduledJob Custom
// Resource projected into an immutable revision. This item is the active
// revision: the revision id, the source identity it came from, and the spec it
// pins. There is no status or version here because the ledger has none — the
// run rows carry lifecycle, and the revision is immutable.
type GetItem struct {
	ID         int64  `json:"id"`
	TenantID   string `json:"tenant_id"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	SourceMode string `json:"source_mode"`
	SourceUID  string `json:"source_uid"`
	Generation int64  `json:"generation"`
	SpecHash   string `json:"spec_hash"`
	// Actor is the identity that projected this revision, recorded when it was
	// written. A later editor changes the active revision, not this row.
	Actor     string    `json:"actor"`
	CreatedAt time.Time `json:"created_at"`

	// The fields below are decoded from the revision's normalized spec.
	Schedule          string      `json:"schedule"`
	Suspend           bool        `json:"suspend"`
	ConcurrencyPolicy string      `json:"concurrency_policy"`
	MisfirePolicy     string      `json:"misfire_policy"`
	TimeoutSeconds    int32       `json:"timeout_seconds"`
	RetryMaxAttempts  int32       `json:"retry_max_attempts"`
	JobTemplate       JobTemplate `json:"job_template"`
	ScheduleSummary   string      `json:"schedule_summary"`
}

// JobTemplate is the container the definition runs per attempt.
type JobTemplate struct {
	Image        string   `json:"image"`
	Command      []string `json:"command,omitempty"`
	Args         []string `json:"args,omitempty"`
	BackoffLimit int32    `json:"backoff_limit"`
}

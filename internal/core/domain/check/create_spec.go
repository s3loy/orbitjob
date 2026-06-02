package check

import "time"

// CreateSpec is the normalized result persisted by the write-side repository.
type CreateSpec struct {
	Name           string
	Description    *string
	TenantID       string
	CheckType      string
	CheckConfig    map[string]any
	AssertionRules []AssertionRule
	ScheduleType   string
	CronExpr       *string
	IntervalSec    *int
	Timezone       string
	TimeoutSec     int
	RetryLimit     int
	Priority       int
	Labels         map[string]any
	NextRunAt      *time.Time
}

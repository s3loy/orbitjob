package check

const (
	DefaultTenantID    = "default"
	DefaultTimezone    = "UTC"
	DefaultTimeoutSec  = 30
	DefaultRetryLimit  = 2
	DefaultPriority    = 5

	ScheduleTypeCron     = "cron"
	ScheduleTypeInterval = "interval"

	CheckTypeHTTPHealth = "http_health"

	StatusActive = "active"
	StatusPaused = "paused"

	ActionPause  = "pause"
	ActionResume = "resume"
)

// CreateInput is the domain input for check creation.
type CreateInput struct {
	Name            string
	Description     *string
	TenantID        string
	CheckType       string
	CheckConfig     map[string]any
	AssertionRules  []AssertionRule
	ScheduleType    string
	CronExpr        *string
	IntervalSec     *int
	Timezone        string
	TimeoutSec      int
	RetryLimit      int
	Priority        int
	Labels          map[string]any
}

// AssertionRule defines a single evaluation rule for the evaluator.
type AssertionRule struct {
	Metric    string  `json:"metric"`
	Operator  string  `json:"operator"`
	Threshold float64 `json:"threshold"`
	Severity  string  `json:"severity"`
}

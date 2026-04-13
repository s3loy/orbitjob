package job

const (
	DefaultTenantID   = "default"
	DefaultTimezone   = "UTC"
	DefaultTimeoutSec = 60

	TriggerTypeCron   = "cron"
	TriggerTypeManual = "manual"

	RetryBackoffFixed       = "fixed"
	RetryBackoffExponential = "exponential"

	ConcurrencyAllow   = "allow"
	ConcurrencyForbid  = "forbid"
	ConcurrencyReplace = "replace"

	MisfireSkip    = "skip"
	MisfireFireNow = "fire_now"
	MisfireCatchUp = "catch_up"
)

// CreateInput is the domain input for job creation.
type CreateInput struct {
	Name                 string
	TenantID             string
	Priority             int
	PartitionKey         *string
	TriggerType          string
	CronExpr             *string
	Timezone             string
	HandlerType          string
	HandlerPayload       map[string]any
	TimeoutSec           int
	RetryLimit           int
	RetryBackoffSec      int
	RetryBackoffStrategy string
	ConcurrencyPolicy    string
	MisfirePolicy        string
}

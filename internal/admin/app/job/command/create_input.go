package command

// CreateInput is the admin command input for creating a job.
type CreateInput struct {
	Name        string
	TenantID    string
	TriggerType string
	CronExpr    *string
	Timezone    string

	HandlerType    string
	HandlerPayload map[string]any

	TimeoutSec           int
	RetryLimit           int
	RetryBackoffSec      int
	RetryBackoffStrategy string
	ConcurrencyPolicy    string
	MisfirePolicy        string
}

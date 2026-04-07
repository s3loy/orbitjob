package command

// UpdateInput is the admin command input for updating a job definition.
type UpdateInput struct {
	ID        int64
	TenantID  string
	ChangedBy string
	Version   int

	Name        string
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

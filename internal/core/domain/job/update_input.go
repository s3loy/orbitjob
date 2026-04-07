package job

// UpdateInput is the core-owned mutable job input.
type UpdateInput struct {
	ID       int64
	TenantID string
	Version  int

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

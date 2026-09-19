package job

import "time"

// CreateSpec is the normalized result persisted by the write-side repository.
type CreateSpec struct {
	Name     string
	TenantID string
	// ResourceGroupID is the isolation group this job belongs to, taken from
	// the creating key's own scope. Empty means ungrouped.
	ResourceGroupID      string
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
	NextRunAt            *time.Time
}

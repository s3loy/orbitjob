package schedule

import "time"

// ScheduledOneResult is the scheduler repository outcome for one claimed due cron job.
type ScheduledOneResult struct {
	JobID     int64
	TenantID  string
	RunID     string
	Created   bool
	NextRunAt *time.Time
}

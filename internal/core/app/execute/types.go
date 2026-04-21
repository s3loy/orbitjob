package execute

import (
	"context"
	"time"
)

type AssignedTask struct {
	InstanceID           int64
	RunID                string
	TenantID             string
	JobID                int64
	HandlerType          string
	HandlerPayload       map[string]any
	TimeoutSec           int
	Priority             int
	Attempt              int
	MaxAttempt           int
	RetryBackoffSec      int
	RetryBackoffStrategy string
	TraceID              *string
	ScheduledAt          time.Time
	LeaseExpiresAt       time.Time
}

type Result struct {
	Success    bool
	ResultCode string
	ErrorMsg   string
}

type Handler interface {
	Execute(ctx context.Context, task AssignedTask) Result
}

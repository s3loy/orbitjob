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
	EffectivePriority    int
	DispatchedAt         time.Time
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
	// Output contains structured handler output for evaluation engines.
	// Only populated by check-type handlers; regular handlers leave this nil.
	Output map[string]any
}

type Handler interface {
	Execute(ctx context.Context, task AssignedTask) Result
}

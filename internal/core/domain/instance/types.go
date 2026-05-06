package instance

import (
	"orbitjob/internal/domain/validation"
	"time"
)

const (
	DefaultTenantID         = "default"
	DefaultIdempotencyScope = "job_instance_create"

	TriggerSourceSchedule = "schedule"
	TriggerSourceManual   = "manual"

	StatusPending     = "pending"
	StatusDispatching = "dispatching"
	StatusDispatched  = "dispatched"
	StatusRunning     = "running"
	StatusRetryWait   = "retry_wait"
	StatusSuccess     = "success"
	StatusFailed      = "failed"
	StatusCanceled    = "canceled"
)

type ValidationError = validation.Error

func IsValidationError(err error) bool {
	return validation.Is(err)
}

func AsValidationError(err error, target **ValidationError) bool {
	return validation.As(err, target)
}

func validationError(field, message string) error {
	return validation.New(field, message)
}

func validationErrorf(field, format string, args ...any) error {
	return validation.Errorf(field, format, args...)
}

type Snapshot struct {
	ID               int64
	RunID            string
	TenantID         string
	JobID            int64
	TriggerSource    string
	Status           string
	Priority          int
	EffectivePriority int
	PartitionKey      *string
	IdempotencyKey   *string
	IdempotencyScope string
	RoutingKey       *string
	WorkerID         *string
	Attempt          int
	MaxAttempt       int
	ScheduledAt      time.Time
	StartedAt        *time.Time
	FinishedAt       *time.Time
	LeaseExpiresAt   *time.Time
	DispatchedAt     *time.Time
	RetryAt          *time.Time
	ResultCode       *string
	ErrorMsg         *string
	TraceID          *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Version          int
}

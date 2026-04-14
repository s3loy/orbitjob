package worker

import (
	"orbitjob/internal/domain/validation"
	"time"
)

const (
	DefaultTenantID = "default"

	StatusOnline   = "online"
	StatusOffline  = "offline"
	StatusDraining = "draining"
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

type Snapshot struct {
	TenantID        string
	WorkerID        string
	Status          string
	LastHeartbeatAt time.Time
	LeaseExpiresAt  time.Time
	Capacity        int
	Labels          map[string]any
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

package job

import (
	"fmt"

	"orbitjob/internal/domain/validation"
)

type ValidationError = validation.Error

func validationError(field, message string) error {
	return validation.New(field, message)
}

func validationErrorf(field, format string, args ...any) error {
	return validation.Errorf(field, format, args...)
}

// QuotaExceededError is returned when a tenant exceeds a quota limit.
type QuotaExceededError struct {
	Quota string
	Limit int
}

func NewQuotaExceededError(quota string, limit int) *QuotaExceededError {
	return &QuotaExceededError{Quota: quota, Limit: limit}
}

func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("quota exceeded: %s (limit=%d)", e.Quota, e.Limit)
}

package job

import (
	"errors"
	"fmt"
)

// ValidationError represents a domain validation failure.
type ValidationError struct {
	Field   string
	Message string
	Cause   error
}

func (e *ValidationError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Field == "" && e.Cause == nil {
		return e.Message
	}
	if e.Field == "" && e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	if e.Cause == nil {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("%s: %s: %v", e.Field, e.Message, e.Cause)
}

func (e *ValidationError) Unwrap() error {
	return e.Cause
}

func IsValidationError(err error) bool {
	var target *ValidationError
	return errors.As(err, &target)
}

func AsValidationError(err error, target **ValidationError) bool {
	return errors.As(err, target)
}

func validationError(field, message string) error {
	return &ValidationError{
		Field:   field,
		Message: message,
	}
}

func validationErrorf(field, format string, args ...any) error {
	return &ValidationError{
		Field:   field,
		Message: fmt.Sprintf(format, args...),
	}
}

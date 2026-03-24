package validation

import (
	"errors"
	"fmt"
)

// Error represents a domain validation failure.
type Error struct {
	Field   string
	Message string
	Cause   error
}

func (e *Error) Error() string {
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

func (e *Error) Unwrap() error {
	return e.Cause
}

func Is(err error) bool {
	var target *Error
	return errors.As(err, &target)
}

func As(err error, target **Error) bool {
	return errors.As(err, target)
}

func New(field, message string) error {
	return &Error{
		Field:   field,
		Message: message,
	}
}

func Errorf(field, format string, args ...any) error {
	return &Error{
		Field:   field,
		Message: fmt.Sprintf(format, args...),
	}
}

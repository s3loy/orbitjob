package job

import (
	"orbitjob/internal/domain/validation"
)

// ValidationError is kept as a compatibility alias while validation errors move
// toward dedicated domain packages.
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

// NotFoundEror indicates a requested resource does not exist.
type NotFoundError struct {
	Resource string
	ID       any
}

func (e *NotFoundError) Error() string {
	return e.Resource + "not found"
}

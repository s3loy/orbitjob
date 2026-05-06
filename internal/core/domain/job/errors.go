package job

import "orbitjob/internal/domain/validation"

type ValidationError = validation.Error

func validationError(field, message string) error {
	return validation.New(field, message)
}

func validationErrorf(field, format string, args ...any) error {
	return validation.Errorf(field, format, args...)
}

package http

import (
	"errors"

	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// ErrorCode is the machine-readable error category, stable for clients to depend on.
type ErrorCode string

const (
	ErrCodeValidation ErrorCode = "VALIDATION_ERROR"
	ErrCodeNotFound   ErrorCode = "NOT_FOUND"
	ErrCodeConflict   ErrorCode = "CONFLICT"
	ErrCodeInternal   ErrorCode = "INTERNAL_ERROR"
)

// APIError is the stable HTTP error response structure.
type APIError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Field   string    `json:"field,omitempty"`
}

func toBindAPIError(_ error) APIError {
	return APIError{
		Code:    ErrCodeValidation,
		Message: "invalid request",
	}
}

// toAPIError maps a domain error to the stable API error structure.
func toAPIError(err error) APIError {
	var ve *validation.Error
	if validation.As(err, &ve) {
		return APIError{
			Code:    ErrCodeValidation,
			Message: ve.Message,
			Field:   ve.Field,
		}
	}

	var ne *resource.NotFoundError
	if errors.As(err, &ne) {
		return APIError{
			Code:    ErrCodeNotFound,
			Message: "resource not found",
			Field:   ne.Resource,
		}
	}

	var ce *resource.ConflictError
	if errors.As(err, &ce) {
		field := ce.Field
		if field == "" {
			field = ce.Resource
		}
		message := ce.Message
		if message == "" {
			message = "resource conflict"
		}

		return APIError{
			Code:    ErrCodeConflict,
			Message: message,
			Field:   field,
		}
	}

	return APIError{
		Code:    ErrCodeInternal,
		Message: "an internal error occurred",
	}
}

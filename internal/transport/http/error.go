package httpapi

import (
	"errors"

	"orbitjob/internal/job"
)

// ErrorCode is the machine-readable error category, stable for clients to depend on.
type ErrorCode string

const (
	ErrCodeValidation ErrorCode = "VALIDATION_ERROR"
	ErrCodeNotFound   ErrorCode = "NOT_FOUND"
	ErrCodeInternal   ErrorCode = "INTERNAL_ERROR"
)

// APIError is the stable HTTP error response structure.
type APIError struct {
	Code    ErrorCode `json:"code"`
	Messgae string    `json:"message"`
	Field   string    `json:"field,omitempty"`
}

// toAPIError maps a domain error to the stable API error structure.
func toAPIError(err error) APIError {
	var ve *job.ValidationError
	if job.AsValidationError(err, &ve) {
		return APIError{
			Code:    ErrCodeValidation,
			Messgae: ve.Message,
			Field:   ve.Field,
		}
	}

	var ne *job.NotFoundError
	if errors.As(err, &ne) {
		return APIError{
			Code:    ErrCodeNotFound,
			Messgae: "resource not found",
			Field:   ne.Resource,
		}
	}

	return APIError{
		Code:    ErrCodeInternal,
		Messgae: "an internal error occurred",
	}
}

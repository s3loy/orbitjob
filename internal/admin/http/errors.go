package http

import (
	"errors"

	"github.com/go-playground/validator/v10"

	"orbitjob/internal/admin/http/apperror"
	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// toBindAPIError maps a Gin binding error to the stable API error structure.
// Validation tag failures become VALIDATION_ERROR; JSON/type/Content-Type
// failures become MALFORMED_REQUEST per PRD 11.2.
func toBindAPIError(err error) apperror.APIError {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		fe := ve[0]
		return apperror.APIError{
			Code:    apperror.CodeValidation,
			Message: fe.Tag(),
			Field:   fe.Field(),
		}
	}

	return apperror.APIError{
		Code:    apperror.CodeMalformedRequest,
		Message: "request could not be parsed",
	}
}

// toAPIError maps a domain/use-case error to the stable API error structure.
func toAPIError(err error) apperror.APIError {
	var ve *validation.Error
	if validation.As(err, &ve) {
		return apperror.APIError{
			Code:    apperror.CodeValidation,
			Message: ve.Message,
			Field:   ve.Field,
		}
	}

	var ne *resource.NotFoundError
	if errors.As(err, &ne) {
		return apperror.APIError{
			Code:    apperror.CodeNotFound,
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

		return apperror.APIError{
			Code:    apperror.CodeConflict,
			Message: message,
			Field:   field,
		}
	}

	var qe *domainjob.QuotaExceededError
	if errors.As(err, &qe) {
		return apperror.APIError{
			Code:    apperror.CodeQuotaExhausted,
			Message: "quota exhausted",
			Field:   qe.Quota,
		}
	}

	return apperror.APIError{
		Code:    apperror.CodeInternal,
		Message: "an internal error occurred",
	}
}

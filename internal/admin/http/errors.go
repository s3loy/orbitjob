package http

import (
	"errors"

	"github.com/go-playground/validator/v10"

	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// toBindAPIError maps a Gin binding error to the stable API error structure.
// Validation tag failures become VALIDATION_ERROR; JSON/type/Content-Type
// failures become MALFORMED_REQUEST per PRD 11.2.
// When multiple validation failures exist, only the first is reported.
func toBindAPIError(err error) apperror.APIError {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) && len(ve) > 0 {
		fe := ve[0]
		return apperror.APIError{
			Code:    apperror.CodeValidation,
			Message: humanReadableTag(fe),
			Field:   fe.Field(),
		}
	}

	return apperror.APIError{
		Code:    apperror.CodeMalformedRequest,
		Message: "request could not be parsed",
	}
}

// humanReadableTag converts a validator tag to a short, human-readable message.
func humanReadableTag(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "field is required"
	case "min":
		return "value is below minimum"
	case "max":
		return "value is above maximum"
	case "oneof":
		return "value must be one of the allowed options"
	case "gt", "gte":
		return "value is too small"
	case "lt", "lte":
		return "value is too large"
	default:
		return "field validation failed"
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

	// A scope that cannot be applied to the resource is a refusal too. The
	// alternative — serving the request without the scope — would hand the
	// caller more than its grant covers, which is the one outcome an
	// authorization check must never produce.
	var se *resource.ScopeError
	if errors.As(err, &se) {
		return apperror.APIError{
			Code:    apperror.CodeForbidden,
			Message: se.Error(),
			Field:   se.Resource,
		}
	}

	// Granting a permission the caller does not itself hold is a refusal, not a
	// server fault: the request was understood and denied.
	if errors.Is(err, policy.ErrPrivilegeEscalation) {
		return apperror.APIError{
			Code:    apperror.CodeForbidden,
			Message: "the request would grant permissions the caller does not hold",
		}
	}

	// A platform preset is installation-wide, so no caller may delete one. That
	// is a refusal too, and one worth stating plainly: a 404 would suggest the
	// policy is simply absent from their view.
	if errors.Is(err, policy.ErrPlatformPolicyImmutable) {
		return apperror.APIError{
			Code:    apperror.CodeForbidden,
			Message: "platform policies cannot be modified",
		}
	}

	return apperror.APIError{
		Code:    apperror.CodeInternal,
		Message: "an internal error occurred",
	}
}

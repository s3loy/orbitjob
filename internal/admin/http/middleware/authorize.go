package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// ResourceResolver maps a request onto the ARN it targets. A resolver that
// cannot determine the target returns an error, which denies the request: an
// unresolvable target must never fall through as permitted.
type ResourceResolver func(*gin.Context) (policy.ARN, error)

// Require rejects the request unless the credential's effective documents
// permit the action on the resource the request names.
//
// The documents are resolved once per request by the auth middleware, already
// narrowed by the key's boundary, so this check is the single place an
// authorization decision is made.
func Require(action string, resolve ResourceResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		docs, ok := Documents(c)
		if !ok {
			// Missing documents means the request never went through auth, or
			// auth failed to load them. Treating that as "no permissions" would
			// hide a wiring defect behind a 403, so it is a server error.
			apperror.Write(c, http.StatusInternalServerError, apperror.APIError{
				Code:    apperror.CodeInternal,
				Message: "authorization context missing",
			})
			c.Abort()
			return
		}

		arn, err := resolve(c)
		if err != nil {
			apiErr := apperror.APIError{Code: apperror.CodeInternal, Message: "resource could not be resolved"}
			var missing *resource.NotFoundError
			var invalid *validation.Error
			switch {
			case errors.Is(err, ErrTenantMismatch), errors.Is(err, ErrTenantRequired), errors.Is(err, ErrResourceUnavailable):
				apiErr = apperror.APIError{Code: apperror.CodeForbidden, Message: "resource is unavailable in the requested scope"}
			case errors.As(err, &missing):
				apiErr = apperror.APIError{Code: apperror.CodeNotFound, Message: "resource not found", Field: missing.Resource}
			case errors.As(err, &invalid):
				apiErr = apperror.APIError{Code: apperror.CodeValidation, Message: invalid.Message, Field: invalid.Field}
			default:
				_ = c.Error(err)
			}
			apperror.Write(c, apperror.StatusForCode(apiErr.Code), apiErr)
			c.Abort()
			return
		}

		if policy.Evaluate(docs, policy.Request{Action: action, Resource: arn}) != policy.DecisionAllow {
			apperror.Write(c, http.StatusForbidden, apperror.APIError{
				Code:    apperror.CodeForbidden,
				Message: "permission denied",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

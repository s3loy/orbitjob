package apperror

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Code is the machine-readable error category, stable for clients to depend on.
type Code string

// PRD 11.2 error codes.
const (
	CodeMalformedRequest   Code = "MALFORMED_REQUEST"
	CodeValidation         Code = "VALIDATION_ERROR"
	CodeUnauthorized       Code = "UNAUTHORIZED"
	CodeForbidden          Code = "FORBIDDEN"
	CodeNotFound           Code = "NOT_FOUND"
	CodeConflict           Code = "CONFLICT"
	CodeRateLimited        Code = "RATE_LIMITED"
	CodeQuotaExhausted     Code = "QUOTA_EXHAUSTED"
	CodeInternal           Code = "INTERNAL_ERROR"
	CodeServiceUnavailable Code = "SERVICE_UNAVAILABLE"
)

// APIError is the stable Admin API error response structure.
type APIError struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// ErrorResponse wraps APIError in the standard envelope.
type ErrorResponse struct {
	Error APIError `json:"error"`
}

// Write writes the error response and aborts the gin context.
// For 429 Too Many Requests it also sets Retry-After: 1.
func Write(c *gin.Context, statusCode int, apiErr APIError) {
	if statusCode == http.StatusTooManyRequests {
		c.Header("Retry-After", "1")
	}
	c.AbortWithStatusJSON(statusCode, ErrorResponse{Error: apiErr})
}

// StatusForCode returns the canonical HTTP status for a PRD error code.
func StatusForCode(code Code) int {
	switch code {
	case CodeMalformedRequest, CodeValidation:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeRateLimited, CodeQuotaExhausted:
		return http.StatusTooManyRequests
	case CodeServiceUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// Package http provides the admin HTTP handler.
package http

import (
	"orbitjob/internal/admin/http"
	"orbitjob/internal/admin/http/apperror"
)

type (
	Handler           = http.Handler
	OpenAPIDocument   = http.OpenAPIDocument
	OpenAPIInfo       = http.OpenAPIInfo
	OpenAPIComponents = http.OpenAPIComponents
	PathItem          = http.PathItem
	Operation         = http.Operation
	Parameter         = http.Parameter
	RequestBody       = http.RequestBody
	Response          = http.Response
	Header            = http.Header
	MediaType         = http.MediaType
	Schema            = http.Schema
	ErrorCode         = apperror.Code
	APIError          = apperror.APIError
)

const (
	ErrCodeMalformedRequest   = apperror.CodeMalformedRequest
	ErrCodeValidation         = apperror.CodeValidation
	ErrCodeUnauthorized       = apperror.CodeUnauthorized
	ErrCodeForbidden          = apperror.CodeForbidden
	ErrCodeNotFound           = apperror.CodeNotFound
	ErrCodeConflict           = apperror.CodeConflict
	ErrCodeRateLimited        = apperror.CodeRateLimited
	ErrCodeQuotaExhausted     = apperror.CodeQuotaExhausted
	ErrCodeInternal           = apperror.CodeInternal
	ErrCodeServiceUnavailable = apperror.CodeServiceUnavailable
)

var (
	NewHandler             = http.NewHandler
	ServiceOpenAPIDocument = http.ServiceOpenAPIDocument
)

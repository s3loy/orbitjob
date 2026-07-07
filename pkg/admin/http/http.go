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
	ErrCodeValidation = string(apperror.CodeValidation)
	ErrCodeNotFound   = string(apperror.CodeNotFound)
	ErrCodeConflict   = string(apperror.CodeConflict)
	ErrCodeInternal   = string(apperror.CodeInternal)
)

var (
	NewHandler             = http.NewHandler
	ServiceOpenAPIDocument = http.ServiceOpenAPIDocument
)

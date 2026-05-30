// Package http provides the admin HTTP handler.
package http

import internal "orbitjob/internal/admin/http"

type (
	Handler           = internal.Handler
	OpenAPIDocument   = internal.OpenAPIDocument
	OpenAPIInfo       = internal.OpenAPIInfo
	OpenAPIComponents = internal.OpenAPIComponents
	PathItem          = internal.PathItem
	Operation         = internal.Operation
	Parameter         = internal.Parameter
	RequestBody       = internal.RequestBody
	Response          = internal.Response
	Header            = internal.Header
	MediaType         = internal.MediaType
	Schema            = internal.Schema
	ErrorCode         = internal.ErrorCode
	APIError          = internal.APIError
)

const (
	ErrCodeValidation = internal.ErrCodeValidation
	ErrCodeNotFound   = internal.ErrCodeNotFound
	ErrCodeConflict   = internal.ErrCodeConflict
	ErrCodeInternal   = internal.ErrCodeInternal
)

var (
	NewHandler             = internal.NewHandler
	ServiceOpenAPIDocument = internal.ServiceOpenAPIDocument
)

package http

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/logarchive"
)

type instanceArchivePath struct {
	RunID    int64  `uri:"run_id" binding:"required,min=1"`
	TenantID string `form:"tenant_id"`
}
type functionArchivePath struct {
	ID       int64  `uri:"id" binding:"required,min=1"`
	RunID    string `uri:"run_id" binding:"required,uuid"`
	TenantID string `form:"tenant_id"`
}
type cleanupReceipt struct {
	State string `json:"state"`
}

func archiveLogRoutes() []routeDefinition {
	var routes []routeDefinition
	for _, function := range []bool{false, true} {
		function := function
		prefix := "/instances/:run_id/logs/archive"
		kind := "instance"
		var params any = instanceArchivePath{}
		if function {
			prefix = "/functions/:id/runs/:run_id/logs/archive"
			kind = "function"
			params = functionArchivePath{}
		}
		for _, operation := range []string{"read", "download", "delete"} {
			operation := operation
			path := prefix
			method := http.MethodGet
			permission := kind + ":ReadLogs"
			content := "application/json"
			var model any = logarchive.Page{}
			code := 200
			if operation == "download" {
				path += "/download"
				content = "text/plain"
				model = ""
			}
			if operation == "delete" {
				method = http.MethodDelete
				permission = kind + ":DeleteLogs"
				model = cleanupReceipt{}
				code = 202
			}
			routes = append(routes, routeDefinition{method: method, path: path, permission: permission, resource: tenantScoped(kind), resourceForHandler: func(h *Handler) middleware.ResourceResolver { return h.archiveResource(function) }, enabled: func(h *Handler) bool { return h != nil && h.archiveDirectory != nil && h.archiveReader != nil }, register: func(r gin.IRouter, h *Handler) {
				if operation == "delete" {
					r.DELETE(path, h.requestArchiveCleanup)
				} else {
					r.GET(path, h.readArchive(operation == "download"))
				}
			}, spec: operationDefinition{id: operation + kind + "ExecutionArchive", summary: operation + " durable execution logs", description: "Authenticated execution evidence across attempts. Cursor reads support bounded reconnectable follow; logs have per-execution and installation byte limits. Protected archives survive Pod cleanup for 15 days after completion/seal. Gaps and capacity rejection are explicit. Delete commits an audited cleanup request and does not claim immediate deletion.", tags: []string{"Logs"}, parameterModels: []any{params, logarchive.Query{}}, responses: []responseDefinition{{statusCode: code, description: "Archive page, retained output export or cleanup receipt", contentType: content, model: model}, {statusCode: 400, description: "Invalid cursor or request", model: apperror.ErrorResponse{}}, {statusCode: 403, description: "Independent log permission denied", model: apperror.ErrorResponse{}}, {statusCode: 404, description: "Archive not registered", model: apperror.ErrorResponse{}}, {statusCode: 503, description: "Archive unavailable", model: apperror.ErrorResponse{}}}}})
		}
	}
	return routes
}

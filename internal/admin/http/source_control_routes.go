package http

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/admin/http/middleware"
)

func sourceControlRoutes() []routeDefinition {
	var routes []routeDefinition
	for _, kind := range []string{"jobs", "workflows", "functions", "checks"} {
		for _, update := range []bool{false, true} {
			method, action, id := http.MethodGet, ":Get", "get"
			if update {
				method, action, id = http.MethodPatch, ":ManageExecution", "update"
			}
			resourceType := strings.TrimSuffix(kind, "s")
			path := "/" + kind + "/:id/execution-controls"
			spec := operationDefinition{id: id + resourceType + "ExecutionControls", summary: id + " source execution controls", description: "Schedule pause blocks new scheduled occurrences. Execution pause also freezes new root runs, workflow steps, and Pending or Prepared dispatch. Possibly started attempts retain recovery and cancellation. Deletion is an independent irreversible identity tombstone. Compare-and-set version zero represents previously unset controls. A lost response requires a read before any new write; equal current state is not an independent historical operation receipt.", tags: []string{"Execution controls"}, parameterModels: []any{sourceControlPath{}}, responses: []responseDefinition{
				{statusCode: 200, description: "Current source controls and exact write capability", model: SourceControlResponse{}},
				{statusCode: 400, description: "Invalid controls", model: apperror.ErrorResponse{}},
				{statusCode: 403, description: "Independent resource permission denied", model: apperror.ErrorResponse{}},
				{statusCode: 404, description: "Parent resource unavailable in this scope", model: apperror.ErrorResponse{}},
				{statusCode: 409, description: "Control version changed; read current controls", model: apperror.ErrorResponse{}},
			}}
			if update {
				spec.requestBodyModel = SourceControlUpdateRequest{}
				spec.requestBodyRequired = true
			}
			routes = append(routes, routeDefinition{method: method, path: path, permission: resourceType + action, resource: tenantScoped(resourceType), resourceForHandler: func(h *Handler) middleware.ResourceResolver { return h.sourceControlResource(kind) }, enabled: func(h *Handler) bool { return h != nil && h.sourceControls != nil }, register: func(r gin.IRouter, h *Handler) {
				if update {
					r.PATCH(path, h.sourceControlHandler(true))
				} else {
					r.GET(path, h.sourceControlHandler(false))
				}
			}, spec: spec})
		}
	}
	return routes
}

package http

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"orbitjob/internal/admin/definition"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/admin/logs"
)

type definitionPath struct {
	Namespace string `uri:"namespace" binding:"required"`
	Name      string `uri:"name" binding:"required"`
	TenantID  string `form:"tenant_id"`
}
type definitionOperationPath struct {
	Namespace string `uri:"namespace" binding:"required"`
	Name      string `uri:"name" binding:"required"`
	RequestID string `uri:"request_id" binding:"required,uuid"`
	TenantID  string `form:"tenant_id"`
}
type instanceLogPath struct {
	RunID    int64  `uri:"run_id" binding:"required,min=1"`
	Attempt  int    `uri:"attempt" binding:"required,min=1"`
	TenantID string `form:"tenant_id"`
}
type functionLogPath struct {
	ID       int64  `uri:"id" binding:"required,min=1"`
	RunID    string `uri:"run_id" binding:"required,uuid"`
	Attempt  int    `uri:"attempt" binding:"required,min=1"`
	TenantID string `form:"tenant_id"`
}

func definitionLogRoutes() []routeDefinition {
	var routes []routeDefinition
	errors := []responseDefinition{{statusCode: 400, description: "Invalid request", model: apperror.ErrorResponse{}}, {statusCode: 403, description: "Denied", model: apperror.ErrorResponse{}}, {statusCode: 404, description: "Not retained or unavailable parent", model: apperror.ErrorResponse{}}, {statusCode: 409, description: "Identity or version conflict", model: apperror.ErrorResponse{}}}
	for _, kind := range []string{"jobs", "workflows"} {
		kind := kind
		typ := "job"
		if kind == "workflows" {
			typ = "workflow"
		}
		path := "/definitions/" + kind + "/:namespace/:name"
		routes = append(routes, routeDefinition{method: http.MethodGet, path: path + "/operations/:request_id", permission: typ + ":Get", resource: definitionARN(kind), enabled: func(h *Handler) bool { return h != nil && h.definitions != nil }, register: func(r gin.IRouter, h *Handler) { r.GET(path+"/operations/:request_id", h.getDefinitionOperation(kind)) }, spec: operationDefinition{id: "get" + typ + "DefinitionOperation", summary: "Read an original definition save outcome", description: "Owner-scoped durable operation history, independently of later definition changes or deletion. Unknown means no automatic write retry. Request references do not grant access.", tags: []string{"Definitions"}, parameterModels: []any{definitionOperationPath{}}, responses: append([]responseDefinition{{statusCode: 200, description: "Retained operation outcome", model: definition.Operation{}}}, errors...)}})
		routes = append(routes, routeDefinition{method: http.MethodPut, path: path + "/compat", permission: typ + ":Apply", resource: definitionARN(kind), enabled: func(h *Handler) bool { return h != nil && h.definitions != nil }, register: func(r gin.IRouter, h *Handler) { r.PUT(path+"/compat", h.applyDefinition(kind)) }, spec: operationDefinition{id: "apply" + typ + "DefinitionCompatible", summary: "Apply a legacy definition with mandatory server checks", description: "Explicit checked-save-v1 adapter. Checks the exact legacy payload on the server; execution changes introduced by admission require an upgraded client's review. Stable request identity and normal authorization still apply.", tags: []string{"Definitions"}, parameterModels: []any{definitionPath{}}, requestBodyModel: definition.ApplyRequest{}, requestBodyRequired: true, responses: append([]responseDefinition{{statusCode: 200, description: "Checked legacy operation", model: definition.Result{}}}, errors...)}})
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			method := method
			action := typ + ":Get"
			id := "get" + typ + "Definition"
			summary := "Read live typed " + typ + " definition"
			var body any
			if method == http.MethodPut {
				action = typ + ":Apply"
				id = "apply" + typ + "Definition"
				summary = "Validate or apply typed " + typ + " definition"
				body = definition.ApplyRequest{}
			}
			routes = append(routes, routeDefinition{method: method, path: path, permission: action, resource: definitionARN(kind), enabled: func(h *Handler) bool { return h != nil && h.definitions != nil }, register: func(r gin.IRouter, h *Handler) {
				if method == http.MethodGet {
					r.GET(path, h.getDefinition(kind))
				} else {
					r.PUT(path, h.applyDefinition(kind))
				}
			}, spec: operationDefinition{id: id, summary: summary, description: "Only configured tenant namespaces and ungrouped credentials. spec_json is a canonical typed JSON string preserving integer precision. PUT dry_run performs Kubernetes admission without persistence; actual writes require its digest, stable request UUID and expected UID/resourceVersion. Accepted CRs do not assert operator projection. Repeating the same accepted request and digest reconciles; changed identity or content conflicts.", tags: []string{"Definitions"}, parameterModels: []any{definitionPath{}}, requestBodyModel: body, requestBodyRequired: method == http.MethodPut, responses: append([]responseDefinition{{statusCode: 200, description: "Live definition, validation, acceptance or reconciliation", model: definition.Result{}}}, errors...)}})
		}
	}
	for _, function := range []bool{false, true} {
		function := function
		path := "/instances/:run_id/attempts/:attempt/logs"
		action := "instance:ReadLogs"
		typ := "instance"
		id := "getInstanceAttemptLogs"
		var params any = instanceLogPath{}
		if function {
			path = "/functions/:id/runs/:run_id/attempts/:attempt/logs"
			action = "function:ReadLogs"
			typ = "function"
			id = "getFunctionAttemptLogs"
			params = functionLogPath{}
		}
		resolver := tenantScoped(typ)
		if !function {
			resolver = instanceLogARN
		}
		routes = append(routes, routeDefinition{method: http.MethodGet, path: path, permission: action, resource: resolver, enabled: func(h *Handler) bool { return h != nil && h.logLocators != nil && h.workloadLogs != nil }, register: func(r gin.IRouter, h *Handler) { r.GET(path, h.readLogs(function)) }, spec: operationDefinition{id: id, summary: "Read bounded retained task logs", description: "attempt is the positive ledger attempt_number, not the attempt row ID. Function run_id is its history UUID, mapped exactly to a ledger execution under the authorized Function parent. No client Pod or namespace locator. Verifies immutable JobRun, Job and Pod ownership; non-streaming task-container snapshot with five-second deadline. Absence is not a durability promise.", tags: []string{"Logs"}, parameterModels: []any{params, logs.Request{}}, responses: append([]responseDefinition{{statusCode: 200, description: "Bounded snapshot or explicit retention/start/availability state", model: logs.Result{}}}, errors...)}})
	}
	return routes
}

package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"orbitjob/internal/admin/http/apperror"
)

type functionTenantQuery struct {
	TenantID string `form:"tenant_id" binding:"omitempty,len=26"`
}

func functionManagementRoutes() []routeDefinition {
	errorModel := apperror.ErrorResponse{}
	return []routeDefinition{
		{
			method: http.MethodPost, path: "/functions", permission: "function:Create", resource: tenantScoped("function"),
			enabled:  func(h *Handler) bool { return h != nil && h.functionManager != nil },
			register: func(r gin.IRouter, h *Handler) { r.POST("/functions", h.CreateFunction) },
			spec: operationDefinition{
				id: "createFunction", summary: "Create a Function definition",
				description: "Create a tenant-owned container Function. Requires independent function:Create access; reads and invokes grant no writes. Tenant is resolved from authenticated authority and tenant_id; resource group is inherited from the key. Unknown body fields, including client-owned tenant/group fields, are rejected. Defaults: active status, 60 second timeout, zero retries, and three retained successes/failures. X-OrbitJob-Idempotency-Key is required; replaying the same normalized definition under the same key and actor returns the original creation snapshot and stable resource identity, even if that definition was edited later. Reusing an identity for different input is a conflict. Creation and audit commit together. Definition version and active revision readiness are independent; ready means the active revision matches the active definition version. Operator projection is asynchronous.",
				tags:        []string{"Functions"}, parameterModels: []any{functionTenantQuery{}, requiredFunctionIdempotencyHeader{}},
				requestBodyModel: FunctionDefinitionRequest{}, requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: http.StatusCreated, description: "Function created", model: FunctionItem{}},
					{statusCode: http.StatusOK, description: "Original Function returned for idempotent replay", model: FunctionItem{}},
					{statusCode: http.StatusBadRequest, description: "Invalid definition or missing request identity", model: errorModel},
					{statusCode: http.StatusForbidden, description: "Function creation denied", model: errorModel},
					{statusCode: http.StatusConflict, description: "Request identity conflicts with earlier input", model: errorModel},
					{statusCode: http.StatusInternalServerError, description: "Internal error; replay with the same request identity after an uncertain response", model: errorModel},
				},
			},
		},
		{
			method: http.MethodPut, path: "/functions/:id", permission: "function:Update", resource: tenantScoped("function"),
			enabled: func(h *Handler) bool {
				return h != nil && h.functionManager != nil && h.functionManagementReader != nil
			},
			register: func(r gin.IRouter, h *Handler) { r.PUT("/functions/:id", h.UpdateFunction) },
			spec: operationDefinition{
				id: "updateFunction", summary: "Replace a Function definition or pause/resume it",
				description: "Replace the complete mutable container definition using required expected_version. Omitted optional settings use creation defaults. Status active or paused supports resume/pause; pausing prevents new calls and does not cancel running invocations. Requires independent function:Update on the persisted tenant and group, including explicit Deny. Tenant and resource group are immutable. A stale version returns conflict; reload and review before retrying. Successful changes increment version and commit with their audit. An active older revision may remain until asynchronous Operator projection; use expected_version on invocation to prevent running it. No deletion or arbitrary invocation payload is supported.",
				tags:        []string{"Functions"}, parameterModels: []any{GetFunctionRequest{}},
				requestBodyModel: UpdateFunctionRequest{}, requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: http.StatusOK, description: "Definition saved; inspect readiness before invoking", model: FunctionItem{}},
					{statusCode: http.StatusBadRequest, description: "Invalid definition or expected version", model: errorModel},
					{statusCode: http.StatusForbidden, description: "Function unavailable in the authorized scope or update denied", model: errorModel},
					{statusCode: http.StatusNotFound, description: "Function no longer exists", model: errorModel},
					{statusCode: http.StatusConflict, description: "The definition version changed", model: errorModel},
					{statusCode: http.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
	}
}

package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	policycommand "orbitjob/internal/admin/app/policy/command"
	groupcommand "orbitjob/internal/admin/app/resourcegroup/command"
	"orbitjob/internal/admin/http/apperror"
)

func tenantManagementRoutes() []routeDefinition {
	errors := []responseDefinition{
		{statusCode: http.StatusBadRequest, description: "Invalid request", model: apperror.ErrorResponse{}},
		{statusCode: http.StatusForbidden, description: "Exact target action denied or requested grant exceeds caller permissions", model: apperror.ErrorResponse{}},
		{statusCode: http.StatusConflict, description: "Resource conflicts with current state", model: apperror.ErrorResponse{}},
		{statusCode: http.StatusInternalServerError, description: "Internal error", model: apperror.ErrorResponse{}},
	}
	routes := []routeDefinition{
		{method: http.MethodGet, path: "/tenants/:id/policies", permission: "policy:List", resource: tenantManagementResource("policy"),
			enabled: func(h *Handler) bool { return h != nil && h.listPoliciesUC != nil }, register: func(r gin.IRouter, h *Handler) { r.GET("/tenants/:id/policies", h.ListTenantPolicies) },
			spec: operationDefinition{id: "listTenantPolicies", summary: "List policies for an explicitly managed tenant", tags: []string{"Policies"}, parameterModels: []any{TenantURI{}}, responses: []responseDefinition{{statusCode: http.StatusOK, description: "Target tenant policies and platform presets", model: policyListResponse{}}}}},
		{method: http.MethodPost, path: "/tenants/:id/policies", permission: "policy:Create", resource: tenantManagementResource("policy"),
			enabled: func(h *Handler) bool { return h != nil && h.createPolicyUC != nil }, register: func(r gin.IRouter, h *Handler) { r.POST("/tenants/:id/policies", h.CreateTenantPolicy) },
			spec: operationDefinition{id: "createTenantPolicy", summary: "Create a policy for an explicitly managed tenant", description: "Requires policy:Create on the exact target collection. The requested grants, with self resolved to that target, must be a subset of the caller's effective permissions including explicit denies and boundary. Does not switch ordinary query scope.", tags: []string{"Policies"}, parameterModels: []any{TenantURI{}}, requestBodyModel: CreatePolicyRequest{}, requestBodyRequired: true, responses: []responseDefinition{{statusCode: http.StatusCreated, description: "Created target policy", model: policycommand.CreateResult{}}}}},
		{method: http.MethodGet, path: "/tenants/:id/resource_groups", permission: "group:List", resource: tenantManagementResource("group"),
			enabled: func(h *Handler) bool { return h != nil && h.listGroupsUC != nil }, register: func(r gin.IRouter, h *Handler) { r.GET("/tenants/:id/resource_groups", h.ListTenantResourceGroups) },
			spec: operationDefinition{id: "listTenantResourceGroups", summary: "List resource groups for an explicitly managed tenant", tags: []string{"Resource Groups"}, parameterModels: []any{TenantURI{}}, responses: []responseDefinition{{statusCode: http.StatusOK, description: "Target tenant resource groups", model: resourceGroupListResponse{}}}}},
		{method: http.MethodPost, path: "/tenants/:id/resource_groups", permission: "group:Create", resource: tenantManagementResource("group"),
			enabled: func(h *Handler) bool { return h != nil && h.createGroupUC != nil }, register: func(r gin.IRouter, h *Handler) { r.POST("/tenants/:id/resource_groups", h.CreateTenantResourceGroup) },
			spec: operationDefinition{id: "createTenantResourceGroup", summary: "Create a resource group for an explicitly managed tenant", description: "Requires group:Create on the exact target collection before target-scoped RLS is opened. Does not grant access to any workload or switch ordinary query scope.", tags: []string{"Resource Groups"}, parameterModels: []any{TenantURI{}}, requestBodyModel: CreateResourceGroupRequest{}, requestBodyRequired: true, responses: []responseDefinition{{statusCode: http.StatusCreated, description: "Created target resource group", model: groupcommand.CreateResult{}}}}},
	}
	for i := range routes {
		routes[i].spec.responses = append(routes[i].spec.responses, errors...)
	}
	return routes
}

package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	policyquery "orbitjob/internal/admin/app/policy/query"
	groupquery "orbitjob/internal/admin/app/resourcegroup/query"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/metrics"
)

// These routes deliberately name their target in the path. The Require guard
// evaluates the caller's effective documents before any target-scoped query.
// The ordinary business tenant resolver is unchanged.
func tenantManagementResource(kind string) middleware.ResourceResolver {
	return func(c *gin.Context) (policy.ARN, error) {
		id := c.Param("id")
		if len(id) != 26 {
			return policy.ARN{}, validation.New("id", "must be a 26-character tenant id")
		}
		if p, ok := middleware.PrincipalFrom(c); !ok {
			return policy.ARN{}, errNoPrincipal
		} else if p.ResourceGroupID != "" && p.TenantID != id {
			return policy.ARN{}, middleware.ErrResourceUnavailable
		}
		if selected := c.Query("tenant_id"); selected != "" && selected != id {
			return policy.ARN{}, middleware.ErrTenantMismatch
		}
		return policy.ARN{Tenant: id, Group: "*", Type: kind, ID: "*"}, nil
	}
}

func (h *Handler) ListTenantPolicies(c *gin.Context) {
	out, err := h.listPoliciesUC.List(c.Request.Context(), policyquery.ListInput{TenantID: c.Param("id")})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, policyListResponse{Items: out})
}
func (h *Handler) CreateTenantPolicy(c *gin.Context) {
	var req CreatePolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, http.StatusBadRequest, toBindAPIError(err))
		return
	}
	doc, err := policy.ParseDocument(req.Document)
	if err != nil {
		writeAPIError(c, validation.New("document", err.Error()))
		return
	}
	if err := policy.ValidateTenantDocument(doc); err != nil {
		writeAPIError(c, validation.New("document", err.Error()))
		return
	}
	callerDocs, ok := middleware.Documents(c)
	if !ok {
		writeAPIError(c, middleware.ErrAuthorizationLookup)
		return
	}
	// Creating a grant document for another workspace cannot become a route
	// around the key creator's delegation and boundary checks. Resolve self to
	// the target, never the caller's home tenant.
	if err := policy.CheckGrantable(callerDocs, []policy.Document{policy.ResolveSelf(doc, c.Param("id"))}); err != nil {
		writeAPIError(c, err)
		return
	}
	out, err := h.createPolicyUC.Create(c.Request.Context(), req.ToCreateInput(c.Param("id"), actorID(c)))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	metrics.PoliciesTotal.WithLabelValues(c.Param("id")).Inc()
	c.JSON(http.StatusCreated, out)
}
func (h *Handler) ListTenantResourceGroups(c *gin.Context) {
	out, err := h.listGroupsUC.List(c.Request.Context(), groupquery.ListInput{TenantID: c.Param("id")})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, resourceGroupListResponse{Items: out})
}
func (h *Handler) CreateTenantResourceGroup(c *gin.Context) {
	var req CreateResourceGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, http.StatusBadRequest, toBindAPIError(err))
		return
	}
	out, err := h.createGroupUC.Create(c.Request.Context(), req.ToCreateInput(c.Param("id"), actorID(c)))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

package http

import (
	"context"
	"errors"
	"net/http"
	"orbitjob/internal/domain/resource"
	"strings"

	"github.com/gin-gonic/gin"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/validation"
)

// IdentityResponse contains only credential metadata and exact policy decisions.
type IdentityResponse struct {
	SubjectType        string               `json:"subject_type"`
	UserID             string               `json:"user_id,omitempty"`
	Username           string               `json:"username,omitempty"`
	DisplayName        string               `json:"display_name,omitempty"`
	InstallationAdmin  bool                 `json:"installation_admin"`
	MembershipRole     string               `json:"membership_role,omitempty"`
	KeyID              string               `json:"key_id"`
	Kind               string               `json:"kind"`
	TenantID           string               `json:"tenant_id"`
	GroupID            string               `json:"group_id"`
	BoundaryPolicyID   string               `json:"boundary_policy_id"`
	TargetTenantID     string               `json:"target_tenant_id,omitempty"`
	TenantName         string               `json:"tenant_name,omitempty"`
	TenantSlug         string               `json:"tenant_slug,omitempty"`
	KeyName            string               `json:"key_name,omitempty"`
	WorkloadNamespaces []string             `json:"workload_namespaces"`
	Capabilities       []IdentityCapability `json:"capabilities"`
}
type IdentityCapability struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Allowed  bool   `json:"allowed"`
}
type IdentityRequest struct {
	TenantID  string   `form:"tenant_id"`
	Actions   []string `form:"action"`
	Resources []string `form:"resource"`
}

func (h *Handler) GetIdentity(c *gin.Context) {
	p, ok := middleware.PrincipalFrom(c)
	if !ok {
		apperror.Write(c, http.StatusUnauthorized, apperror.APIError{Code: apperror.CodeUnauthorized, Message: "authentication required"})
		c.Abort()
		return
	}
	docs, ok := middleware.Documents(c)
	if !ok {
		apperror.Write(c, http.StatusInternalServerError, apperror.APIError{Code: apperror.CodeInternal, Message: "authorization context missing"})
		c.Abort()
		return
	}
	var req IdentityRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, http.StatusBadRequest, toBindAPIError(err))
		return
	}
	out := IdentityResponse{SubjectType: p.SubjectType, UserID: p.UserID, Username: p.Username, DisplayName: p.DisplayName, InstallationAdmin: p.InstallationAdmin, MembershipRole: p.MembershipRole, KeyID: p.KeyID, Kind: p.Kind, TenantID: p.TenantID, GroupID: p.ResourceGroupID, BoundaryPolicyID: p.BoundaryPolicyID, Capabilities: []IdentityCapability{}, WorkloadNamespaces: []string{}}
	tenantID := ""
	if (!p.IsPlatform() && !p.IsHuman()) || req.TenantID != "" {
		var err error
		tenantID, err = middleware.ResolveEffectiveTenant(c, req.TenantID)
		if err != nil {
			writeForbidden(c, err.Error())
			return
		}
	}
	if len(req.Actions) != len(req.Resources) || len(req.Actions) > 64 {
		writeAPIError(c, validation.New("capabilities", "supply at most 64 paired action and resource values"))
		return
	}
	out.TargetTenantID = tenantID
	for i, action := range req.Actions {
		arn, err := policy.ParseARN(req.Resources[i])
		management := err == nil && isManagementCollectionProbe(action, arn)
		if err != nil || (!management && (tenantID == "" || arn.Tenant != tenantID)) || strings.ContainsAny(action, "*?") || !policy.IsKnownAction(action) {
			writeAPIError(c, validation.New("capabilities", "invalid target action or resource"))
			return
		}
		probeDocs := docs
		if management && arn.Type == "tenant" && p.IsHuman() && p.InstallationAdmin {
			probeDocs = installationManagementDocuments(docs)
		}
		allowed := policy.Evaluate(probeDocs, policy.Request{Action: action, Resource: arn}) == policy.DecisionAllow
		// A group-bound credential cannot use explicit tenant management to
		// cross its tenant, even when its document contains a broad Allow.
		if management && arn.Tenant != "*" && p.ResourceGroupID != "" && arn.Tenant != p.TenantID {
			allowed = false
		}
		// Resource groups restrict row visibility independently of the grant document.
		if !management && p.ResourceGroupID != "" && arn.Group != p.ResourceGroupID {
			allowed = false
		}
		out.Capabilities = append(out.Capabilities, IdentityCapability{Action: action, Resource: arn.String(), Allowed: allowed})
	}
	if tenantID != "" {
		if err := h.identityMetadata(c.Request.Context(), p, docs, tenantID, &out); err != nil {
			writeAPIError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, out)
}

// The existing SECURITY DEFINER function returns only an owner tenant. It must
// run after authentication and before evaluating the exact key resource.
type apiKeyOwnerReader interface {
	FindKeyTenant(context.Context, string) (string, error)
}

func (h *Handler) SetAPIKeyOwnerReader(r apiKeyOwnerReader) { h.apiKeyOwnerReader = r }
func (h *Handler) revokeKeyResource(c *gin.Context) (policy.ARN, error) {
	p, ok := middleware.PrincipalFrom(c)
	if !ok {
		return policy.ARN{}, errNoPrincipal
	}
	if h.apiKeyOwnerReader == nil {
		return policy.ARN{}, middleware.ErrAuthorizationLookup
	}
	tenant, err := h.apiKeyOwnerReader.FindKeyTenant(c.Request.Context(), c.Param("id"))
	// Foreign and absent keys intentionally produce the same authorization target
	// and therefore the same denial; no ownership details reach the response.
	if err != nil {
		var missing *resource.NotFoundError
		if !errors.As(err, &missing) {
			return policy.ARN{}, middleware.ErrAuthorizationLookup
		}
		return policy.ARN{}, middleware.ErrResourceUnavailable
	}
	if tenant == "" || (!p.IsPlatform() && tenant != p.TenantID) {
		return policy.ARN{}, middleware.ErrResourceUnavailable
	}
	return policy.ARN{Tenant: tenant, Group: "*", Type: "apikey", ID: c.Param("id")}, nil
}

// Only collection actions served by explicit management routes may probe
// outside the business tenant. A probe never loads foreign labels or directories.
func isManagementCollectionProbe(action string, arn policy.ARN) bool {
	if arn.Group != "*" || arn.ID != "*" {
		return false
	}
	if arn.Type == "tenant" {
		return arn.Tenant == "*" && (action == "tenant:Create" || action == "tenant:List")
	}
	if len(arn.Tenant) != 26 || strings.ContainsAny(arn.Tenant, "*?") {
		return false
	}
	switch arn.Type {
	case "policy":
		return action == "policy:Create" || action == "policy:List"
	case "group":
		return action == "group:Create" || action == "group:List"
	case "apikey":
		return action == "apikey:Create" || action == "apikey:List"
	}
	return false
}

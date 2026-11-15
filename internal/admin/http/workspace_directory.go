package http

import (
	"context"
	"net/http"
	"orbitjob/internal/domain/queryfilter"
	"strings"

	"github.com/gin-gonic/gin"
	"orbitjob/internal/admin/app/account"
	query "orbitjob/internal/admin/app/workspace/query"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/admin/http/middleware"
)

type workspaceDirectoryLister interface {
	List(context.Context, query.ListInput) (query.WorkspaceListResult, error)
}

func (h *Handler) SetWorkspaceDirectoryLister(lister workspaceDirectoryLister) {
	h.workspaceDirectory = lister
}

type ListWorkspacesRequest struct {
	Search string `form:"search"`
	Limit  int    `form:"limit,default=20" binding:"min=1,max=100"`
	Offset int    `form:"offset" binding:"omitempty,min=0,max=2147483647"`
}

// ListWorkspaces is authenticated, filtered discovery like identity metadata.
// There is no global List gate: own labels are implicit identity information,
// while each foreign result requires exact tenant:Get in effective documents.
func (h *Handler) ListWorkspaces(c *gin.Context) {
	p, ok := middleware.PrincipalFrom(c)
	if !ok {
		apperror.Write(c, http.StatusUnauthorized, apperror.APIError{Code: apperror.CodeUnauthorized, Message: "authentication required"})
		return
	}
	docs, ok := middleware.Documents(c)
	if !ok {
		apperror.Write(c, http.StatusInternalServerError, apperror.APIError{Code: apperror.CodeInternal, Message: "authorization context missing"})
		return
	}
	var request ListWorkspacesRequest
	if err := c.ShouldBindQuery(&request); err != nil {
		apperror.Write(c, http.StatusBadRequest, toBindAPIError(err))
		return
	}
	if p.IsHuman() {
		if h.accounts == nil {
			accountError(c, account.ErrForbidden)
			return
		}
		search, err := queryfilter.Search(request.Search)
		if err != nil {
			writeAPIError(c, err)
			return
		}
		memberships, err := h.accounts.Service.Workspaces(c.Request.Context(), p.UserID)
		if err != nil {
			accountError(c, err)
			return
		}
		filtered := []query.WorkspaceListItem{}
		for _, workspace := range memberships {
			if search == "" || strings.Contains(strings.ToLower(workspace.Name), strings.ToLower(search)) || strings.Contains(strings.ToLower(workspace.Slug), strings.ToLower(search)) {
				filtered = append(filtered, query.WorkspaceListItem{ID: workspace.ID, Name: workspace.Name, Slug: workspace.Slug, Status: "active"})
			}
		}
		offset := request.Offset
		if offset > len(filtered) {
			offset = len(filtered)
		}
		end := offset + request.Limit
		if end > len(filtered) {
			end = len(filtered)
		}
		c.JSON(http.StatusOK, query.WorkspaceListResult{Items: filtered[offset:end], HasMore: end < len(filtered)})
		return
	}
	out, err := h.workspaceDirectory.List(c.Request.Context(), query.ListInput{Search: request.Search, Limit: request.Limit, Offset: request.Offset, Caller: query.Caller{TenantID: p.TenantID, ResourceGroupID: p.ResourceGroupID, IsPlatform: p.IsPlatform(), Documents: docs}})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
func workspaceDirectoryRoutes() []routeDefinition {
	return []routeDefinition{{method: http.MethodGet, path: "/workspaces", enabled: func(h *Handler) bool { return h != nil && h.workspaceDirectory != nil }, register: func(r gin.IRouter, h *Handler) { r.GET("/workspaces", h.ListWorkspaces) }, spec: operationDefinition{
		id: "listWorkspaces", summary: "Discover authorized workspace display metadata",
		description: "Authenticated metadata discovery, independent of business tenant selectors and the legacy tenant list. Own tenant labels are implicit identity information. Foreign items require effective tenant:Get on orbitjob:<target>:*:tenant/<target>, including Deny, boundary and caller-resolved self; group-bound credentials see only their own workspace. Platform credentials have no implicit own workspace and need no selected target. The tenant_id parameter never changes discovery scope. Search is a case-insensitive literal name/slug substring, trimmed and limited to 128 Unicode characters; percent, underscore and backslash are literal. Authorization and search precede stable lower(name),id pagination. limit defaults to 20, maximum 100. has_more describes only visible matches; no global total is disclosed. Discovery grants no workload access or management operation.",
		tags:        []string{"Workspaces"}, parameterModels: []any{ListWorkspacesRequest{}}, responses: []responseDefinition{
			{statusCode: http.StatusOK, description: "Authorized workspace directory page", model: query.WorkspaceListResult{}},
			{statusCode: http.StatusBadRequest, description: "Invalid search or pagination", model: apperror.ErrorResponse{}},
			{statusCode: http.StatusInternalServerError, description: "Directory unavailable", model: apperror.ErrorResponse{}},
		},
	}}}
}

package http

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"orbitjob/internal/admin/app/account"
	"orbitjob/internal/admin/http/apperror"
	"strings"
)

func accountRoutes() []routeDefinition {
	type endpoint struct {
		method, path, id, summary string
		handle                    func(*AccountHTTP, *gin.Context)
		request, response         any
		status                    int
		public                    bool
	}
	endpoints := []endpoint{
		{http.MethodGet, "/session", "getConsoleSession", "Read the current human session", (*AccountHTTP).getSession, nil, SessionSnapshot{}, 200, true},
		{http.MethodPost, "/login", "loginConsole", "Authenticate a human account", (*AccountHTTP).login, LoginRequest{}, SessionSnapshot{}, 200, true},
		{http.MethodPost, "/logout", "logoutConsole", "Revoke the current human session", (*AccountHTTP).logout, nil, nil, 204, false},
		{http.MethodPost, "/reauth", "reauthenticateConsole", "Confirm the account password for sensitive changes", (*AccountHTTP).reauth, PasswordRequest{}, SessionSnapshot{}, 200, false},
		{http.MethodPost, "/setup", "setupConsole", "Consume the installer capability and create the first administrator", (*AccountHTTP).setup, SetupAccountRequest{}, SessionSnapshot{}, 200, true},
		{http.MethodPost, "/invitations/accept", "acceptConsoleInvitation", "Consume a workspace invitation", (*AccountHTTP).acceptInvitation, SetupAccountRequest{}, SessionSnapshot{}, 200, true},
		{http.MethodPost, "/password/change", "changeConsolePassword", "Change the current account password and revoke every session", (*AccountHTTP).changePassword, PasswordChangeRequest{}, nil, 204, false},
		{http.MethodPost, "/password/reset", "resetConsolePassword", "Consume a single-use password reset capability", (*AccountHTTP).resetPassword, ResetPasswordRequest{}, nil, 204, true},
		{http.MethodGet, "/sessions", "listConsoleSessions", "List the current account sessions", (*AccountHTTP).sessions, nil, SessionListResponse{}, 200, false},
		{http.MethodDelete, "/sessions/:id", "revokeConsoleSession", "Revoke one of the current account sessions", (*AccountHTTP).revokeSession, nil, nil, 204, false},
		{http.MethodGet, "/members", "listConsoleMembers", "List workspace members", (*AccountHTTP).members, nil, MemberListResponse{}, 200, false},
		{http.MethodPost, "/invitations", "inviteConsoleMember", "Issue a single-use workspace invitation", (*AccountHTTP).invite, account.Grant{}, account.Capability{}, 201, false},
		{http.MethodPatch, "/members/:user_id", "updateConsoleMember", "Change a workspace membership", (*AccountHTTP).updateMember, MemberChangeRequest{}, nil, 204, false},
		{http.MethodPost, "/members/:user_id/password-reset", "issueConsolePasswordReset", "Issue a single-use account password reset", (*AccountHTTP).issuePasswordReset, PasswordResetRequest{}, account.Capability{}, 201, false},
		{http.MethodGet, "/users", "listConsoleUsers", "List installation accounts", (*AccountHTTP).users, nil, UserListResponse{}, 200, false},
		{http.MethodPatch, "/users/:id", "updateConsoleUser", "Change an installation account", (*AccountHTTP).updateUser, account.UserChange{}, nil, 204, false},
	}
	out := make([]routeDefinition, 0, len(endpoints))
	for _, e := range endpoints {
		ep := e
		parameters := []any{}
		if strings.Contains(ep.path, ":user_id") {
			parameters = append(parameters, struct {
				UserID string `uri:"user_id" binding:"required"`
			}{})
		} else if strings.Contains(ep.path, ":id") {
			parameters = append(parameters, struct {
				ID string `uri:"id" binding:"required"`
			}{})
		}
		if ep.path == "/members" {
			parameters = append(parameters, struct {
				TenantID string `form:"tenant_id" binding:"required"`
			}{})
		}
		out = append(out, routeDefinition{method: ep.method, path: "/auth" + ep.path, enabled: func(h *Handler) bool { return h != nil && h.accounts != nil }, register: func(r gin.IRouter, h *Handler) {
			r.Handle(ep.method, "/auth"+ep.path, func(c *gin.Context) {
				c.Header("Cache-Control", "no-store")
				c.Header("Pragma", "no-cache")
				if origin := c.GetHeader("Origin"); origin != "" && !h.accounts.Security.OriginAllowed(c.Request) {
					accountError(c, account.ErrForbidden)
					return
				}
				if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
					accountError(c, account.ErrForbidden)
					return
				}
				ep.handle(h.accounts, c)
			})
		}, spec: operationDefinition{id: ep.id, summary: ep.summary, description: "Human account endpoint. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account changes are evaluated against current server state. Sensitive changes require password confirmation within 15 minutes. API keys cannot administer human accounts.", tags: []string{"Authentication"}, parameterModels: parameters, requestBodyModel: ep.request, requestBodyRequired: ep.request != nil, responses: []responseDefinition{{statusCode: ep.status, description: "Account operation completed", model: ep.response}, {statusCode: 403, description: "Operation forbidden, CSRF rejected, or recent_auth_required", model: apperror.ErrorResponse{}}, {statusCode: 409, description: "Conflicting state, including last administrator protection", model: apperror.ErrorResponse{}}}}})
	}
	return out
}

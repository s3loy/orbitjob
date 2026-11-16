package http

import (
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/admin/app/account"
	"orbitjob/internal/admin/http/apperror"
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
		{http.MethodPost, "/invitations/accept", "acceptConsoleInvitation", "Consume a workspace invitation", (*AccountHTTP).acceptInvitation, AcceptInvitationRequest{}, SessionSnapshot{}, 200, true},
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
		}, spec: operationDefinition{id: ep.id, summary: ep.summary, description: accountOperationDescription(ep.id), tags: []string{"Authentication"}, parameterModels: parameters, requestBodyModel: ep.request, requestBodyRequired: ep.request != nil, responses: []responseDefinition{{statusCode: ep.status, description: "Account operation completed", model: ep.response}, {statusCode: 403, description: "Operation forbidden, CSRF rejected, or recent_auth_required where required", model: apperror.ErrorResponse{}}, {statusCode: 409, description: "Conflicting state, including last administrator protection", model: apperror.ErrorResponse{}}}}})
	}
	return out
}

func accountOperationDescription(id string) string {
	var authority string
	switch id {
	case "getConsoleSession":
		authority = "Read installation initialization state and, when a valid session cookie is present, the current account, active workspace memberships and CSRF token. No login is required for an unauthenticated snapshot."
	case "loginConsole":
		authority = "Authenticate an active account with its username and current password, and create a session. Password confirmation starts the 15-minute recent-authentication window."
	case "logoutConsole":
		authority = "Revoke the caller's current session and clear its cookie. Requires that session; no recent password confirmation is required."
	case "reauthenticateConsole":
		authority = "Confirm the current account password for the caller's active session and renew its 15-minute recent-authentication window. An expired recent-authentication window does not prevent this operation."
	case "setupConsole":
		authority = "Consume a valid, unexpired installer setup or recovery capability to create or recover an installation administrator and its default-workspace administrator membership. Requires the capability and account credentials; no existing session is required. A recovery capability can be bound to a username."
	case "acceptConsoleInvitation":
		authority = "Consume a valid, unexpired workspace invitation. Creating a new account requires a username and new password and signs in that account. An existing account must use its own active session cookie and CSRF token; username may be omitted or must match that session, and password may be omitted. Existing account credentials and display name are unchanged. A username-bound invitation must match the accepting account. No recent password confirmation is required."
	case "changeConsolePassword":
		authority = "Change the caller's password using its active session and current password. The operation verifies the supplied current password without requiring a previous recent-authentication window, revokes all of that account's sessions and outstanding reset capabilities, and clears the current cookie. Sign in again after success."
	case "resetConsolePassword":
		authority = "Consume a valid, unexpired single-use reset capability and set a new account password. No session is required. Success revokes all of that account's sessions and outstanding reset capabilities and clears the current cookie."
	case "listConsoleSessions":
		authority = "List unexpired sessions belonging to the caller's account. Requires an active session; no recent password confirmation is required."
	case "revokeConsoleSession":
		authority = "Revoke a session belonging to the caller's account, including the current session. Requires an active session with password confirmation within 15 minutes. Other accounts' sessions cannot be revoked through this operation."
	case "listConsoleMembers":
		authority = "List members of the workspace selected by tenant_id. Requires an active administrator membership in that active workspace and password confirmation within 15 minutes. Installation administrator status alone does not grant workspace membership access."
	case "inviteConsoleMember":
		authority = "Issue a single-use workspace invitation valid for 24 hours. Requires an active administrator membership in the selected active workspace and password confirmation within 15 minutes. Granted permissions must remain within the caller's effective grants and boundary. Installation administrator status alone does not grant this authority."
	case "updateConsoleMember":
		authority = "Change a member's status, role, policies or boundary in the selected workspace. Requires an active administrator membership in that active workspace and password confirmation within 15 minutes. Changed grants and reactivation must remain within the caller's effective grants and boundary. Installation administrator status alone does not grant this authority."
	case "issueConsolePasswordReset":
		authority = "Issue a single-use password reset capability valid for one hour for the selected installation account. Requires installation administrator status and password confirmation within 15 minutes. Workspace administrator membership alone is insufficient; tenant_id does not narrow this installation-scoped authority. Issuing a new reset capability invalidates earlier outstanding reset capabilities for that account."
	case "listConsoleUsers":
		authority = "List installation accounts. Requires installation administrator status and password confirmation within 15 minutes; workspace administrator membership alone is insufficient."
	case "updateConsoleUser":
		authority = "Change an installation account's status, installation administrator flag or display name. Requires installation administrator status and password confirmation within 15 minutes. Disabling an account revokes its sessions. The last active installation administrator cannot be disabled or demoted."
	}
	return authority + " Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts."
}

// Account handlers validate through the service and strict JSON decoder rather
// than Gin binding tags. Describe those constraints without changing decoding.
func applyAccountSchemaMetadata(t reflect.Type, mode schemaMode, schema *Schema) {
	if mode != schemaModeRequest {
		return
	}
	newPassword := "New password: valid UTF-8 with at least 15 Unicode code points and at most 1024 UTF-8 bytes. Common passwords and repeated patterns are rejected; no character-class requirement applies."
	currentPassword := "Current account password. At most 1024 UTF-8 bytes; login and password confirmation do not apply the new-password length or quality rules."
	username := "Account username, normalized by trimming surrounding whitespace and converting to lowercase. The normalized value must match [a-z0-9][a-z0-9._-]{2,63} (3 to 64 ASCII characters)."
	switch t {
	case reflect.TypeFor[LoginRequest]():
		schema.Required = []string{"username", "password"}
		describeProperty(schema, "username", username, nil)
		describeProperty(schema, "password", currentPassword, nil)
		describeProperty(schema, "remember", "Use a seven-day session instead of the default eight-hour session.", false)
	case reflect.TypeFor[SetupAccountRequest]():
		schema.Required = []string{"token", "username", "password"}
		describeProperty(schema, "username", username, nil)
		describeProperty(schema, "password", newPassword, nil)
	case reflect.TypeFor[AcceptInvitationRequest]():
		schema.Required = []string{"token"}
		schema.Description = "A new account requires username and password. An existing account requires its own active session and CSRF token; username and password may be omitted. Supplied nonempty passwords are validated, but never replace an existing account's password."
		describeProperty(schema, "username", username+" Required for a new account; optional for an existing account and must match its authenticated session when supplied.", nil)
		describeProperty(schema, "password", newPassword+" Required for a new account. An existing account may omit this field or send an empty string; a supplied nonempty value is validated and does not change its password.", nil)
	case reflect.TypeFor[PasswordRequest]():
		schema.Required = []string{"password"}
		describeProperty(schema, "password", currentPassword, nil)
	case reflect.TypeFor[PasswordChangeRequest]():
		schema.Required = []string{"current_password", "password"}
		describeProperty(schema, "current_password", currentPassword, nil)
		describeProperty(schema, "password", newPassword, nil)
	case reflect.TypeFor[ResetPasswordRequest]():
		schema.Required = []string{"token", "password"}
		describeProperty(schema, "password", newPassword, nil)
	case reflect.TypeFor[MemberChangeRequest]():
		schema.Required = []string{"tenant_id"}
		describeProperty(schema, "status", "Set status to active or disabled. Omit or use null to keep the current status.", nil)
		describeProperty(schema, "role", "Set role to admin, operator or viewer. Omit or use null to keep the current role.", nil)
		describeProperty(schema, "policy_ids", "Explicit IDs replace the current policy bindings. An empty array resets bindings to the selected role's preset. Omit or use null to retain current bindings unless role changes, which selects the new role's preset. The resulting grant must fit the administrator's effective permissions and boundary.", nil)
	case reflect.TypeFor[PasswordResetRequest]():
		describeProperty(schema, "tenant_id", "Optional compatibility field. Password reset issuance requires installation administrator authority regardless of this value.", nil)
	case reflect.TypeFor[account.Grant]():
		schema.Required = []string{"tenant_id", "role"}
		setPropertyEnum(schema, "role", []string{account.RoleAdmin, account.RoleOperator, account.RoleViewer})
		describeProperty(schema, "username", username+" Omit to allow any eligible account to accept the invitation.", nil)
		describeProperty(schema, "policy_ids", "Optional explicit policy IDs. Omitted or empty uses the selected role's preset; the resulting grant must be contained within the caller's effective permissions.", nil)
		describeProperty(schema, "boundary_policy_id", "Optional boundary policy ID. An empty value inherits the inviting administrator's membership boundary; delegated grants must preserve that boundary.", nil)
	case reflect.TypeFor[account.UserChange]():
		describeProperty(schema, "status", "Set status to active or disabled. Omit or use null to keep the current status.", nil)
	default:
		return
	}
	schema.AdditionalProperties = false
	if token, ok := schema.Properties["token"]; ok {
		token.MinLength, token.MaxLength = intPointer(43), intPointer(43)
		token.Description = "Single-use capability token containing 43 ASCII characters, as issued by the installer or account API. It must be unexpired and unconsumed."
		schema.Properties["token"] = token
	}
	if password, ok := schema.Properties["password"]; ok {
		password.Format = "password"
		if t == reflect.TypeFor[SetupAccountRequest]() || t == reflect.TypeFor[PasswordChangeRequest]() || t == reflect.TypeFor[ResetPasswordRequest]() {
			password.MinLength = intPointer(15)
		}
		schema.Properties["password"] = password
	}
	if name, ok := schema.Properties["display_name"]; ok {
		name.MaxLength = intPointer(128)
		name.Description = "Display name encoded as valid UTF-8, with at most 128 Unicode code points."
		schema.Properties["display_name"] = name
	}
}

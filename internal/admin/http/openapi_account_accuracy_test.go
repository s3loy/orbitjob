package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"orbitjob/internal/admin/app/account"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/logarchive"
	"orbitjob/internal/platform/scan"
)

func TestAccountRequestSchemasExposeServiceValidation(t *testing.T) {
	doc := ServiceOpenAPIDocument()
	for _, tc := range []struct {
		name     string
		required []string
	}{
		{"LoginRequest", []string{"username", "password"}},
		{"SetupAccountRequest", []string{"token", "username", "password"}},
		{"AcceptInvitationRequest", []string{"token"}},
		{"PasswordRequest", []string{"password"}},
		{"PasswordChangeRequest", []string{"current_password", "password"}},
		{"ResetPasswordRequest", []string{"token", "password"}},
		{"MemberChangeRequest", []string{"tenant_id"}},
		{"Grant", []string{"tenant_id", "role"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema, ok := doc.Components.Schemas[tc.name]
			if !ok {
				t.Fatalf("missing request schema %s", tc.name)
			}
			if !slices.Equal(schema.Required, tc.required) {
				t.Errorf("required = %v, want %v", schema.Required, tc.required)
			}
			if schema.AdditionalProperties != false {
				t.Error("account request schema must describe strict unknown-field rejection")
			}
		})
	}
	for _, name := range []string{"SetupAccountRequest", "ResetPasswordRequest", "PasswordChangeRequest"} {
		password := doc.Components.Schemas[name].Properties["password"]
		if password.MinLength == nil || *password.MinLength != 15 {
			t.Errorf("%s must describe the 15-code-point minimum", name)
		}
		if password.MaxLength != nil {
			t.Errorf("%s must not express a UTF-8 byte limit as a character limit", name)
		}
		for _, requirement := range []string{"Unicode code points", "1024 UTF-8 bytes", "Common passwords and repeated patterns"} {
			if !strings.Contains(password.Description, requirement) {
				t.Errorf("%s password description omits %q", name, requirement)
			}
		}
	}
	for _, name := range []string{"LoginRequest", "PasswordRequest"} {
		password := doc.Components.Schemas[name].Properties["password"]
		if password.MinLength != nil || password.MaxLength != nil {
			t.Errorf("%s must not inherit new-password character-count constraints", name)
		}
		if !strings.Contains(password.Description, "do not apply the new-password length or quality rules") {
			t.Errorf("%s must distinguish credential verification from new-password policy", name)
		}
	}
	for _, name := range []string{"SetupAccountRequest", "AcceptInvitationRequest", "ResetPasswordRequest"} {
		token := doc.Components.Schemas[name].Properties["token"]
		if token.MinLength == nil || token.MaxLength == nil || *token.MinLength != 43 || *token.MaxLength != 43 {
			t.Errorf("%s must expose the capability token length", name)
		}
	}
	setup := doc.Paths[adminAPIPrefix+"/auth/setup"].Post.RequestBody.Content["application/json"].Schema
	invitation := doc.Paths[adminAPIPrefix+"/auth/invitations/accept"].Post.RequestBody.Content["application/json"].Schema
	if setup.Ref == invitation.Ref {
		t.Fatal("setup and invitation must not share unconditional credential requirements")
	}
	accept := doc.Components.Schemas["AcceptInvitationRequest"]
	if password := accept.Properties["password"]; password.MinLength != nil || !strings.Contains(password.Description, "empty string") {
		t.Error("an existing account must be able to omit its password or send an empty string")
	}
	for _, condition := range []string{"new account requires username and password", "existing account requires its own active session and CSRF token"} {
		if !strings.Contains(accept.Description, condition) {
			t.Errorf("invitation schema omits conditional requirement %q", condition)
		}
	}
	if maximum := doc.Components.Schemas["SetupAccountRequest"].Properties["display_name"].MaxLength; maximum == nil || *maximum != 128 {
		t.Error("display_name must expose its 128-code-point maximum")
	}
}

func TestAccountPatchNullFieldsPreserveOmittedValues(t *testing.T) {
	doc := ServiceOpenAPIDocument()
	for _, target := range []struct{ schema, field string }{
		{"MemberChangeRequest", "status"},
		{"MemberChangeRequest", "role"},
		{"UserChange", "status"},
	} {
		property := doc.Components.Schemas[target.schema].Properties[target.field]
		if !property.Nullable || len(property.Enum) != 0 {
			t.Errorf("%s.%s must allow null without a string-only enum: %+v", target.schema, target.field, property)
		}
		if !strings.Contains(property.Description, "Omit or use null to keep the current "+target.field) {
			t.Errorf("%s.%s must explain null and omission", target.schema, target.field)
		}
	}
	userID, tenantID := scan.GenerateID(), scan.GenerateID()
	for _, tc := range []struct {
		name, path, body, operation string
	}{
		{"member omitted", "/members/" + userID, `{"tenant_id":"` + tenantID + `"}`, "update_member"},
		{"member null", "/members/" + userID, `{"tenant_id":"` + tenantID + `","status":null,"role":null}`, "update_member"},
		{"user omitted", "/users/" + userID, `{}`, "update_user"},
		{"user null", "/users/" + userID, `{"status":null}`, "update_user"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := strings.Repeat("a", 43)
			session := account.Session{ID: scan.GenerateID(), User: account.User{ID: scan.GenerateID()}, RecentAuthAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
			updated := false
			repo := accountRepositoryStub{call: func(_ context.Context, operation string, input, output any) error {
				if operation == "authenticate" {
					return accountOutput(output, session)
				}
				if operation != tc.operation {
					t.Fatalf("unexpected operation %q; null fields must not trigger grant changes", operation)
				}
				params := input.(map[string]any)
				change, err := json.Marshal(params["change"])
				if err != nil {
					t.Fatal(err)
				}
				if string(change) != "{}" {
					t.Errorf("omitted/null patch fields must remain absent at the repository boundary, got %s", change)
				}
				updated = true
				return nil
			}}
			router, _ := accountRouter(t, repo)
			req := consoleRequest(http.MethodPatch, tc.path, tc.body)
			req.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: token})
			req.Header.Set("X-CSRF-Token", invitationCSRF(token))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != http.StatusNoContent || !updated {
				t.Fatalf("status=%d updated=%t body=%s", response.Code, updated, response.Body)
			}
		})
	}
}

func TestAccountOperationAuthorityIsSpecific(t *testing.T) {
	for _, route := range accountRoutes() {
		t.Run(route.spec.id, func(t *testing.T) {
			description := route.spec.description
			for _, shared := range []string{"HttpOnly SameSite=Strict", "exact configured Origin", "X-CSRF-Token", "API keys cannot administer human accounts"} {
				if !strings.Contains(description, shared) {
					t.Errorf("missing shared account rule %q", shared)
				}
			}
			recent := slices.Contains([]string{"revokeConsoleSession", "listConsoleMembers", "inviteConsoleMember", "updateConsoleMember", "issueConsolePasswordReset", "listConsoleUsers", "updateConsoleUser"}, route.spec.id)
			if got := strings.Contains(description, "password confirmation within 15 minutes"); got != recent {
				t.Errorf("recent-authentication requirement = %t, want %t", got, recent)
			}
			switch route.spec.id {
			case "listConsoleMembers", "inviteConsoleMember", "updateConsoleMember":
				if !strings.Contains(description, "active administrator membership") || !strings.Contains(description, "Installation administrator status alone") {
					t.Error("workspace management must require explicit active workspace administrator membership")
				}
			case "issueConsolePasswordReset", "listConsoleUsers", "updateConsoleUser":
				if !strings.Contains(description, "Requires installation administrator status") {
					t.Error("installation account management must state installation authority")
				}
			case "revokeConsoleSession":
				if !strings.Contains(description, "belonging to the caller's account") {
					t.Error("session revocation must state self-account scope")
				}
			}
		})
	}
}

func TestArchivePageDefaultsFollowParameterModel(t *testing.T) {
	doc := ServiceOpenAPIDocument()
	archiveReads := 0
	for path, item := range doc.Paths {
		if !strings.Contains(path, "/logs/archive") || item.Get == nil {
			continue
		}
		archiveReads++
		limit := parameterSchema(item.Get.Parameters, "limit", "query")
		if limit.Default != 500 {
			t.Errorf("GET %s: archive limit default = %v, want 500", path, limit.Default)
		}
		if limit.Maximum == nil || *limit.Maximum != 1000 {
			t.Errorf("GET %s: archive limit maximum must remain 1000", path)
		}
	}
	if archiveReads != 4 {
		t.Fatalf("checked %d archive read/export operations, want 4", archiveReads)
	}
	for _, tc := range []struct {
		model any
		want  any
	}{
		{ListJobsRequest{}, 50},
		{logarchive.Query{}, 500},
		{struct {
			Limit int `form:"limit"`
		}{}, nil},
		{struct {
			Limit int `form:"limit,default=20"`
		}{}, float64(20)},
	} {
		got := parameterSchema(parametersFromModel(tc.model), "limit", "query").Default
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%T limit default = %v, want %v", tc.model, got, tc.want)
		}
	}
}

func TestExecutionReferenceDescriptionsPreserveCurrentBoundaries(t *testing.T) {
	doc := ServiceOpenAPIDocument()
	for _, tc := range []struct {
		path     string
		post     bool
		required []string
		obsolete []string
	}{
		{"/workflows", false, []string{"projected active revision", "PUT /api/v1/definitions/workflows/{namespace}/{name}", "after operator projection"}, []string{"there is no create route", "declared outside this API"}},
		{"/instances/{run_id}/cancel", true, []string{"Prepared attempt before dispatch", "trusted termination evidence", "original owned Kubernetes Job no longer exists", "completed release", "phase observed at request time"}, []string{"Canceled phase only once the operator has observed its Kubernetes Job gone"}},
		{"/functions/{id}/invoke", true, []string{"new Kubernetes Job and Pod", "asynchronous invocation receipt", "wait budget", "does not limit the workload's execution timeout", "no log text or function response body"}, []string{"honest seconds", "not milliseconds"}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			item := doc.Paths[adminAPIPrefix+tc.path]
			op := item.Get
			if tc.post {
				op = item.Post
			}
			if op == nil {
				t.Fatal("missing operation")
			}
			for _, required := range tc.required {
				if !strings.Contains(op.Description, required) {
					t.Errorf("description omits %q", required)
				}
			}
			for _, obsolete := range tc.obsolete {
				if strings.Contains(op.Description, obsolete) {
					t.Errorf("description retains obsolete claim %q", obsolete)
				}
			}
		})
	}
}

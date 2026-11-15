//go:build integration

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/admin/app/account"
	"orbitjob/internal/admin/bootstrap"
)

// This journey uses the deployed database role, real HTTP auth and policy
// repositories, and an in-memory Kubernetes client for the invocation actor.
func TestHumanAccountHTTPJourney(t *testing.T) {
	server, db, legacy, stores := newIntegrationServerWithIdentity(t, true)
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	admin := &http.Client{Jar: jar}
	request := func(client *http.Client, method, path string, body any, csrf string) *http.Response {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, err := http.NewRequest(method, server.URL+"/api/v1"+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", server.URL)
		req.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	snapshot := func(client *http.Client) SessionSnapshot {
		t.Helper()
		resp := request(client, "GET", "/auth/session", nil, "")
		requireStatus(t, resp, 200)
		var out SessionSnapshot
		decodeJSON(t, resp, &out)
		return out
	}
	if out := snapshot(admin); out.Initialized || out.Authenticated {
		t.Fatal("unclaimed installation unexpectedly authenticated")
	}
	password := "A personal account passphrase 42"
	cap, err := stores.accounts.IssueInstallationCapability(context.Background(), "setup", "", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	resp := request(admin, "POST", "/auth/setup", SetupAccountRequest{Token: cap.Token, Username: "installer-admin", Password: password, DisplayName: "Installer Admin"}, "")
	requireStatus(t, resp, 200)
	var auth SessionSnapshot
	decodeJSON(t, resp, &auth)
	if !auth.Authenticated || !auth.Initialized || auth.User == nil || !auth.User.InstallationAdmin || len(auth.Workspaces) != 1 || auth.Workspaces[0].ID != bootstrap.DefaultTenantID || auth.CSRFToken == "" {
		t.Fatal("setup did not establish explicit default membership and session")
	}
	if restored := snapshot(admin); restored.User.ID != auth.User.ID || restored.CSRFToken != auth.CSRFToken {
		t.Fatal("restoration changed stable human or CSRF identity")
	}
	resp = request(admin, "GET", "/identity", nil, "")
	requireStatus(t, resp, 200)
	var identity IdentityResponse
	decodeJSON(t, resp, &identity)
	if identity.KeyID != "" || identity.UserID != auth.User.ID || identity.SubjectType != "user" || identity.Kind == "platform" || identity.TargetTenantID != "" {
		t.Fatal("unselected human identity invented a key or platform scope")
	}
	resp = request(admin, "POST", "/policies?tenant_id="+bootstrap.DefaultTenantID, map[string]any{"name": "CSRF rejected"}, "")
	requireStatus(t, resp, 403)
	resp.Body.Close()
	if _, err := db.Exec(`UPDATE account_sessions SET recent_auth_at=clock_timestamp()-INTERVAL '1 hour' WHERE user_id=$1`, auth.User.ID); err != nil {
		t.Fatal(err)
	}
	policyBody := map[string]any{"name": "Human audit grant", "document": map[string]any{"version": "1", "statement": []any{map[string]any{"effect": "Allow", "action": []string{"job:List"}, "resource": []string{"orbitjob:self:*:job/*"}}}}}
	resp = request(admin, "POST", "/policies?tenant_id="+bootstrap.DefaultTenantID, policyBody, auth.CSRFToken)
	requireStatus(t, resp, 403)
	resp.Body.Close()
	resp = request(admin, "POST", "/auth/reauth", PasswordRequest{Password: password}, auth.CSRFToken)
	requireStatus(t, resp, 200)
	decodeJSON(t, resp, &auth)
	resp = request(admin, "POST", "/policies?tenant_id="+bootstrap.DefaultTenantID, policyBody, auth.CSRFToken)
	requireStatus(t, resp, 201)
	var created struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &created)
	var actorType, actorID string
	if err := db.QueryRow(`SELECT actor_type,actor_id FROM audit_events WHERE resource_id=$1 AND resource_type='policy'`, created.ID).Scan(&actorType, &actorID); err != nil {
		t.Fatal(err)
	}
	if actorType != "user" || actorID != auth.User.ID {
		t.Fatalf("human audit actor type=%s", actorType)
	}
	stores.functions.seed(functionIntegrationDef(3, bootstrap.DefaultTenantID))
	resp = request(admin, "POST", "/functions/3/invoke?tenant_id="+bootstrap.DefaultTenantID, nil, auth.CSRFToken)
	requireStatus(t, resp, 201)
	var invoked struct{ Namespace, Name string }
	decodeJSON(t, resp, &invoked)
	obj, err := stores.tracker().Get(jobRunGVR, invoked.Namespace, invoked.Name)
	if err != nil {
		t.Fatal(err)
	}
	var run v1alpha1.JobRun
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(*unstructured.Unstructured).Object, &run); err != nil {
		t.Fatal(err)
	}
	if run.Spec.Actor != auth.User.ID {
		t.Fatal("run did not preserve the authenticated human actor")
	}
	resp = request(admin, "POST", "/tenants", CreateTenantRequest{Name: "Human workspace", Slug: "human-workspace"}, auth.CSRFToken)
	requireStatus(t, resp, 201)
	var workspace account.Workspace
	decodeJSON(t, resp, &workspace)
	var memberships int
	if err := db.QueryRow(`SELECT count(*) FROM account_memberships WHERE user_id=$1 AND tenant_id=$2 AND role='admin' AND status='active'`, auth.User.ID, workspace.ID).Scan(&memberships); err != nil || memberships != 1 {
		t.Fatal("workspace creation omitted its explicit creator membership")
	}
	resp = request(admin, "POST", "/auth/invitations", account.Grant{TenantID: bootstrap.DefaultTenantID, Role: "viewer"}, auth.CSRFToken)
	requireStatus(t, resp, 201)
	var invitation account.Capability
	decodeJSON(t, resp, &invitation)
	viewerJar, _ := cookiejar.New(nil)
	viewer := &http.Client{Jar: viewerJar}
	resp = request(viewer, "POST", "/auth/invitations/accept", SetupAccountRequest{Token: invitation.Token, Username: "invited-viewer", Password: password}, "")
	requireStatus(t, resp, 200)
	var viewerSession SessionSnapshot
	decodeJSON(t, resp, &viewerSession)
	resp = request(viewer, "GET", "/identity?tenant_id="+workspace.ID, nil, "")
	requireStatus(t, resp, 403)
	resp.Body.Close()
	resp = request(admin, "POST", "/auth/members/"+viewerSession.User.ID+"/password-reset", PasswordResetRequest{}, auth.CSRFToken)
	requireStatus(t, resp, 201)
	var reset account.Capability
	decodeJSON(t, resp, &reset)
	resp = request(viewer, "POST", "/auth/password/reset", ResetPasswordRequest{Token: reset.Token, Password: password + " changed"}, "")
	requireStatus(t, resp, 204)
	resp.Body.Close()
	if snapshot(viewer).Authenticated {
		t.Fatal("password reset retained a usable session")
	}
	resp = request(viewer, "POST", "/auth/login", LoginRequest{Username: "invited-viewer", Password: password + " changed"}, "")
	requireStatus(t, resp, 200)
	decodeJSON(t, resp, &viewerSession)
	resp = request(viewer, "POST", "/auth/password/change", PasswordChangeRequest{CurrentPassword: password + " changed", Password: password + " changed twice"}, viewerSession.CSRFToken)
	requireStatus(t, resp, 204)
	resp.Body.Close()
	if snapshot(viewer).Authenticated {
		t.Fatal("self password change retained a usable session")
	}
	resp = request(viewer, "POST", "/auth/login", LoginRequest{Username: "invited-viewer", Password: password + " changed twice"}, "")
	requireStatus(t, resp, 200)
	decodeJSON(t, resp, &viewerSession)
	resp = request(admin, "PATCH", "/auth/members/"+viewerSession.User.ID, MemberChangeRequest{TenantID: bootstrap.DefaultTenantID, Status: stringPtr("disabled")}, auth.CSRFToken)
	requireStatus(t, resp, 204)
	resp.Body.Close()
	resp = request(viewer, "GET", "/identity?tenant_id="+bootstrap.DefaultTenantID, nil, "")
	requireStatus(t, resp, 403)
	resp.Body.Close()
	resp = request(admin, "PATCH", "/auth/users/"+viewerSession.User.ID, account.UserChange{Status: stringPtr("disabled")}, auth.CSRFToken)
	requireStatus(t, resp, 204)
	resp.Body.Close()
	if snapshot(viewer).Authenticated {
		t.Fatal("disabled account retained a usable session")
	}
	// Existing automation continues to authenticate independently of human cookies.
	resp = get(t, server.Client(), server.URL+"/api/v1/identity", legacy)
	requireStatus(t, resp, 200)
	decodeJSON(t, resp, &identity)
	if identity.KeyID == "" || identity.SubjectType != "api_key" {
		t.Fatal("legacy bearer identity changed")
	}
	resp = request(admin, "POST", "/auth/logout", nil, auth.CSRFToken)
	requireStatus(t, resp, 204)
	resp.Body.Close()
	if snapshot(admin).Authenticated {
		t.Fatal("logout session remained usable")
	}
	var sessions int
	if err := db.QueryRow(`SELECT count(*) FROM account_sessions WHERE user_id=$1`, auth.User.ID).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatal(fmt.Sprintf("logout session rows=%d", sessions))
	}
}
func stringPtr(s string) *string { return &s }

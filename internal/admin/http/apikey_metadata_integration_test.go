//go:build integration

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

// The real API authenticates through the unchanged helper and checks the new
// operation against a tenant's current explicit administrator preset.
func TestAPIKeyNameLifecyclePreservesCredentialAuthority(t *testing.T) {
	server, db, bootstrap := newIntegrationServer(t)
	defer server.Close()
	// The fixture restores presets after truncation using the baseline seed.
	// Replay the migration's preset update verbatim to qualify today's preset.
	raw, err := os.ReadFile("../../../db/migrations/0010_api_key_names.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	update := strings.Index(string(raw), "UPDATE policies")
	if update < 0 {
		t.Fatal("missing preset migration")
	}
	if _, err := db.ExecContext(context.Background(), string(raw)[update:]); err != nil {
		t.Fatal(err)
	}

	tenantID := createTenant(t, server, bootstrap, "metadata-operations")
	_, adminKey := createKey(t, server, bootstrap, tenantID, []string{tenantAdminPolicyID}, "")
	resp := postJSON(t, server.Client(), server.URL+"/api/v1/tenants/"+tenantID+"/api_keys", adminKey, map[string]any{"name": "Daily operator"})
	requireStatus(t, resp, http.StatusCreated)
	var created struct {
		ID, Key, Name string
		KeyPrefix     string `json:"key_prefix"`
	}
	decodeJSON(t, resp, &created)
	if created.Name != "Daily operator" || created.Key == "" {
		t.Fatal("name or credential missing")
	}

	readState := func() string {
		var state string
		if err := db.QueryRowContext(context.Background(), `SELECT (to_jsonb(k)-'name')::text FROM api_keys k WHERE id=$1`, created.ID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	before := readState()
	resp = get(t, server.Client(), server.URL+"/api/v1/identity", created.Key)
	requireStatus(t, resp, http.StatusOK)
	var identity IdentityResponse
	decodeJSON(t, resp, &identity)
	if identity.TenantSlug != "metadata-operations" || identity.TenantName == "" || identity.KeyName != "Daily operator" || identity.TargetTenantID != tenantID {
		t.Fatalf("own labels unavailable %+v", identity)
	}
	resp = get(t, server.Client(), server.URL+"/api/v1/tenants/"+tenantID, created.Key)
	requireStatus(t, resp, http.StatusForbidden)
	_ = resp.Body.Close()

	patch := func(token, name string) *http.Response {
		body, _ := json.Marshal(map[string]string{"name": name})
		req, err := http.NewRequest(http.MethodPatch, server.URL+"/api/v1/api_keys/"+created.ID, strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	for _, tc := range []struct {
		key, name string
		status    int
	}{
		{created.Key, "Self escalation", 403}, {bootstrap, "Foreign rename", 403},
		{adminKey, "\nBad label", 400}, {adminKey, "Night operator", 200},
	} {
		resp = patch(tc.key, tc.name)
		requireStatus(t, resp, tc.status)
		_ = resp.Body.Close()
	}
	if after := readState(); after != before {
		t.Fatal("renaming changed credential, grants or lifecycle fields")
	}
	resp = get(t, server.Client(), server.URL+"/api/v1/identity", created.Key)
	requireStatus(t, resp, http.StatusOK)
	decodeJSON(t, resp, &identity)
	if identity.KeyName != "Night operator" {
		t.Fatalf("own key alias not refreshed %+v", identity)
	}
	resp = get(t, server.Client(), server.URL+"/api/v1/tenants/"+tenantID+"/api_keys", adminKey)
	requireStatus(t, resp, http.StatusOK)
	var listing struct{ Items []struct{ ID, Name string } }
	decodeJSON(t, resp, &listing)
	found := false
	for _, item := range listing.Items {
		if item.ID == created.ID {
			found = item.Name == "Night operator"
		}
	}
	if !found {
		t.Fatal("renamed label missing from list")
	}
	resp = get(t, server.Client(), server.URL+"/api/v1/jobs", created.Key)
	requireStatus(t, resp, http.StatusForbidden)
	_ = resp.Body.Close()
	var count int
	var diff string
	err = db.QueryRowContext(context.Background(), `SELECT count(*), COALESCE(min(diff::text),'') FROM audit_events WHERE resource_id=$1 AND event_type='update'`, created.ID).Scan(&count, &diff)
	if err != nil || count != 1 || diff != `{"name": "Night operator"}` {
		t.Fatalf("audit count=%d diff=%s err=%v", count, diff, err)
	}
	resp = patch(adminKey, "")
	requireStatus(t, resp, http.StatusOK)
	_ = resp.Body.Close()
	resp = postJSON(t, server.Client(), server.URL+"/api/v1/api_keys/"+created.ID+"/revoke", adminKey, map[string]any{})
	requireStatus(t, resp, http.StatusOK)
	_ = resp.Body.Close()
	resp = get(t, server.Client(), server.URL+"/api/v1/identity", created.Key)
	requireStatus(t, resp, http.StatusUnauthorized)
	_ = resp.Body.Close()
}

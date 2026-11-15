//go:build integration

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"orbitjob/internal/admin/bootstrap"
	"orbitjob/internal/platform/scan"
)

func TestPolicyUnsupportedSemanticsRejectBeforePersistence(t *testing.T) {
	server, db, key := newIntegrationServer(t)
	defer server.Close()
	const statement = `{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}`
	for _, raw := range []string{
		`{"version":"1","statement":[` + strings.TrimSuffix(statement, "}") + `,"Condition":{"StringEquals":{"environment":"prod"}}}]}`,
		`{"version":"1","statement":[` + strings.TrimSuffix(statement, "}") + `,"NotAction":["job:Trigger"]}]}`,
		`{"version":"1","statement":[{"effect":"Deny","effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`,
		`{"version":"1","statement":[{"effect":"Deny","Effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`,
	} {
		resp := postJSON(t, server.Client(), server.URL+"/api/v1/policies", key, map[string]any{"name": "Unsupported advanced policy", "document": json.RawMessage(raw)})
		requireStatus(t, resp, http.StatusBadRequest)
		_ = resp.Body.Close()
	}
	var policies, events int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM policies WHERE tenant_id=$1`, bootstrap.DefaultTenantID).Scan(&policies); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_type='policy'`, bootstrap.DefaultTenantID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if policies != 0 || events != 0 {
		t.Fatalf("rejected policy persisted or audited as created: policies=%d events=%d", policies, events)
	}
}

func TestStoredUnsupportedPolicyFailsClosedDuringAuthentication(t *testing.T) {
	server, db, administrator := newIntegrationServer(t)
	defer server.Close()
	tenantID := createTenant(t, server, administrator, "legacy-conditional-policy")
	keyID, key := createKey(t, server, administrator, tenantID, nil, "")
	id := scan.GenerateID()
	raw := `{"version":"1","statement":[{"effect":"Allow","action":["job:List"],"resource":["orbitjob:self:*:job/*"],"Condition":{"StringEquals":{"environment":"prod"}}}]}`
	if _, err := db.ExecContext(context.Background(), `INSERT INTO policies(id,tenant_id,name,document)VALUES($1,$2,'Unsupported legacy condition',$3::jsonb)`, id, tenantID, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO key_policies(key_id,policy_id)VALUES($1,$2)`, keyID, id); err != nil {
		t.Fatal(err)
	}
	resp := get(t, server.Client(), server.URL+"/api/v1/jobs", key)
	requireStatus(t, resp, http.StatusServiceUnavailable)
	_ = resp.Body.Close()
	// Identity must not turn a malformed stored document into a working session.
	resp = get(t, server.Client(), server.URL+"/api/v1/identity", key)
	requireStatus(t, resp, http.StatusServiceUnavailable)
	_ = resp.Body.Close()
}

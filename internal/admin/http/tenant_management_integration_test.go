//go:build integration

package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"orbitjob/internal/admin/bootstrap"
	"orbitjob/internal/platform/scan"
)

func seedJourneyPolicy(t *testing.T, db *sql.DB, tenant, name, document string) string {
	t.Helper()
	id := scan.GenerateID()
	if _, err := db.Exec(`INSERT INTO policies(id,tenant_id,name,document)VALUES($1,$2,$3,$4::jsonb)`, id, tenant, name, document); err != nil {
		t.Fatal(err)
	}
	return id
}
func createManagedPolicy(t *testing.T, s *httptest.Server, key, tenant, name, document string, want int) string {
	t.Helper()
	resp := postJSON(t, s.Client(), fmt.Sprintf("%s/api/v1/tenants/%s/policies", s.URL, tenant), key, map[string]any{"name": name, "document": json.RawMessage(document)})
	requireStatus(t, resp, want)
	if want != http.StatusCreated {
		_ = resp.Body.Close()
		return ""
	}
	var out struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &out)
	return out.ID
}
func TestTenantOwnedInstallationAdminOnboardsOneConsistentWorkspace(t *testing.T) {
	s, db, admin := newIntegrationServer(t)
	defer s.Close()
	target := createTenant(t, s, admin, "managed-target")
	policyID := createManagedPolicy(t, s, admin, target, "Operator", `{"version":"1","statement":[{"effect":"Allow","action":["job:List","job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`, http.StatusCreated)
	resp := postJSON(t, s.Client(), s.URL+"/api/v1/tenants/"+target+"/resource_groups", admin, map[string]any{"name": "Production", "slug": "prod"})
	requireStatus(t, resp, http.StatusCreated)
	_ = resp.Body.Close()
	for _, path := range []string{"policies", "resource_groups"} {
		resp = get(t, s.Client(), s.URL+"/api/v1/tenants/"+target+"/"+path, admin)
		requireStatus(t, resp, http.StatusOK)
		var out struct {
			Items []struct {
				ID string `json:"id"`
			}
		}
		decodeJSON(t, resp, &out)
		if len(out.Items) == 0 {
			t.Fatalf("target %s directory is empty", path)
		}
	}
	_, operator := createKey(t, s, admin, target, []string{policyID}, "")
	resp = get(t, s.Client(), s.URL+"/api/v1/jobs", operator)
	requireStatus(t, resp, http.StatusOK)
	_ = resp.Body.Close()
	for _, path := range []string{"policies", "resource_groups"} {
		resp = get(t, s.Client(), s.URL+"/api/v1/tenants/"+target+"/"+path, operator)
		requireStatus(t, resp, http.StatusForbidden)
		_ = resp.Body.Close()
	}
	// Ordinary tenant query selection remains tied to the key's own tenant.
	resp = get(t, s.Client(), s.URL+"/api/v1/jobs?tenant_id="+bootstrap.DefaultTenantID, operator)
	requireStatus(t, resp, http.StatusForbidden)
	_ = resp.Body.Close()
	var actual string
	if err := db.QueryRowContext(context.Background(), `SELECT tenant_id FROM policies WHERE id=$1`, policyID).Scan(&actual); err != nil || actual != target {
		t.Fatalf("policy scope=%s error=%v", actual, err)
	}
}
func TestScopedPolicyCreationRespectsCallerDenyBoundaryAndTarget(t *testing.T) {
	s, db, admin := newIntegrationServer(t)
	defer s.Close()
	home := createTenant(t, s, admin, "management-home")
	target := createTenant(t, s, admin, "management-target")
	grants := `{"version":"1","statement":[{"effect":"Allow","action":["policy:Create","policy:List","job:Get"],"resource":["orbitjob:` + target + `:*:*/*"]},{"effect":"Deny","action":["job:Get"],"resource":["orbitjob:` + target + `:*:job/8"]}]}`
	grantID := seedJourneyPolicy(t, db, home, "Narrow manager", grants)
	_, manager := createKey(t, s, admin, home, []string{grantID}, "")
	makeDoc := func(action, arn string) string {
		return `{"version":"1","statement":[{"effect":"Allow","action":["` + action + `"],"resource":["` + arn + `"]}]}`
	}
	createManagedPolicy(t, s, manager, target, "Allowed self", makeDoc("job:Get", "orbitjob:self:*:job/7"), http.StatusCreated)
	createManagedPolicy(t, s, manager, target, "Denied resource", makeDoc("job:Get", "orbitjob:self:*:job/8"), http.StatusForbidden)
	createManagedPolicy(t, s, manager, target, "Denied broad grant", makeDoc("job:Get", "orbitjob:self:*:job/*"), http.StatusForbidden)
	createManagedPolicy(t, s, manager, target, "Denied action", makeDoc("job:Trigger", "orbitjob:self:*:job/7"), http.StatusForbidden)
	createManagedPolicy(t, s, manager, home, "Wrong target", makeDoc("job:Get", "orbitjob:self:*:job/7"), http.StatusForbidden)
	boundary := seedJourneyPolicy(t, db, home, "Creation ceiling", makeDoc("policy:Create", "orbitjob:"+target+":*:policy/*"))
	_, bounded := createKey(t, s, admin, home, []string{administratorPolicyID}, boundary)
	createManagedPolicy(t, s, bounded, target, "Boundary blocked", makeDoc("job:Get", "orbitjob:self:*:job/7"), http.StatusForbidden)
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM policies WHERE tenant_id=$1`, target).Scan(&count); err != nil || count != 1 {
		t.Fatalf("refused documents persisted: count=%d err=%v", count, err)
	}
	resp := get(t, s.Client(), s.URL+"/api/v1/tenants/"+target+"/policies?tenant_id="+home, manager)
	requireStatus(t, resp, http.StatusForbidden)
	_ = resp.Body.Close()
}

func TestPlatformManagementUsesExplicitTargetWithoutBusinessSelector(t *testing.T) {
	s, db, admin := newIntegrationServer(t)
	defer s.Close()
	target := createTenant(t, s, admin, "platform-managed")
	// Convert only this isolated fixture credential to the existing platform
	// kind; its effective documents still come through the real auth loader.
	if _, err := db.Exec(`UPDATE api_keys SET kind='platform',tenant_id=NULL WHERE id=$1`, bootstrap.DefaultAPIKeyID); err != nil {
		t.Fatal(err)
	}
	var platform int
	if err := db.QueryRow(`SELECT count(*) FROM api_keys WHERE kind='platform'`).Scan(&platform); err != nil || platform != 1 {
		t.Fatalf("platform fixture count=%d error=%v", platform, err)
	}
	createManagedPolicy(t, s, admin, target, "Platform operator", `{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`, http.StatusCreated)
	resp := get(t, s.Client(), s.URL+"/api/v1/tenants/"+target+"/policies", admin)
	requireStatus(t, resp, http.StatusOK)
	_ = resp.Body.Close()

}

func TestGroupBoundWildcardCannotManageForeignTenant(t *testing.T) {
	s, db, admin := newIntegrationServer(t)
	defer s.Close()
	home := createTenant(t, s, admin, "group-home")
	foreign := createTenant(t, s, admin, "group-foreign")
	resp := postJSON(t, s.Client(), s.URL+"/api/v1/tenants/"+home+"/resource_groups", admin, map[string]any{"name": "Scoped group", "slug": "scope"})
	requireStatus(t, resp, http.StatusCreated)
	var group struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &group)
	resp = postJSON(t, s.Client(), s.URL+"/api/v1/tenants/"+home+"/api_keys", admin, map[string]any{"policies": []string{administratorPolicyID}, "resource_group_id": group.ID})
	requireStatus(t, resp, http.StatusCreated)
	var credential struct {
		Key string `json:"key"`
	}
	decodeJSON(t, resp, &credential)
	for _, kind := range []string{"policies", "resource_groups"} {
		resp = get(t, s.Client(), s.URL+"/api/v1/tenants/"+foreign+"/"+kind, credential.Key)
		requireStatus(t, resp, http.StatusForbidden)
		_ = resp.Body.Close()
		resp = get(t, s.Client(), s.URL+"/api/v1/tenants/"+home+"/"+kind, credential.Key)
		requireStatus(t, resp, http.StatusOK)
		_ = resp.Body.Close()
	}
	doc := `{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`
	createManagedPolicy(t, s, credential.Key, foreign, "Forbidden grouped manager", doc, http.StatusForbidden)
	createManagedPolicy(t, s, credential.Key, home, "Own grouped manager", doc, http.StatusCreated)
	resp = postJSON(t, s.Client(), s.URL+"/api/v1/tenants/"+foreign+"/resource_groups", credential.Key, map[string]any{"name": "Forbidden group", "slug": "forbidden"})
	requireStatus(t, resp, http.StatusForbidden)
	_ = resp.Body.Close()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM policies WHERE tenant_id=$1`, foreign).Scan(&count); err != nil || count != 0 {
		t.Fatalf("foreign write count=%d err=%v", count, err)
	}
}

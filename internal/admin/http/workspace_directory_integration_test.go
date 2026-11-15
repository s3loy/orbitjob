//go:build integration

package http

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	query "orbitjob/internal/admin/app/workspace/query"
	"orbitjob/internal/admin/bootstrap"
	"orbitjob/internal/platform/scan"
)

func readWorkspacePage(t *testing.T, s *httptest.Server, key, parameters string) query.WorkspaceListResult {
	t.Helper()
	resp := get(t, s.Client(), s.URL+"/api/v1/workspaces"+parameters, key)
	requireStatus(t, resp, http.StatusOK)
	var out query.WorkspaceListResult
	decodeJSON(t, resp, &out)
	if out.Items == nil {
		t.Fatal("directory items must be an array")
	}
	return out
}
func assertWorkspaceIDs(t *testing.T, out query.WorkspaceListResult, ids ...string) {
	t.Helper()
	got := []string{}
	for _, item := range out.Items {
		got = append(got, item.ID)
	}
	sort.Strings(got)
	sort.Strings(ids)
	if fmt.Sprint(got) != fmt.Sprint(ids) || out.HasMore {
		t.Fatalf("directory=%+v want=%v", out, ids)
	}
}
func workspaceTestPolicy(t *testing.T, db *sql.DB, owner any, name, body string) string {
	t.Helper()
	id := scan.GenerateID()
	if _, err := db.Exec(`INSERT INTO policies(id,tenant_id,name,document)VALUES($1,$2,$3,$4::jsonb)`, id, owner, name, body); err != nil {
		t.Fatal(err)
	}
	return id
}
func tenantReadDocument(effect string, targets ...string) string {
	values := []string{}
	for _, id := range targets {
		values = append(values, `"orbitjob:`+id+`:*:tenant/`+id+`"`)
	}
	return `{"version":"1","statement":[{"effect":"` + effect + `","action":["tenant:Get"],"resource":[` + strings.Join(values, ",") + `]}]}`
}
func TestWorkspaceDirectoryGlobalReloadSearchAndVisiblePagination(t *testing.T) {
	s, db, admin := newIntegrationServer(t)
	defer s.Close()
	expected := map[string]bool{bootstrap.DefaultTenantID: true}
	for index := 0; index < 25; index++ {
		id := fmt.Sprintf("%026d", 100+index)
		expected[id] = true
		name := fmt.Sprintf("Team %02d", index)
		slug := fmt.Sprintf("workspace-%02d", index)
		if index == 3 {
			name = "Literal%_\\ \u4e2d"
		}
		if index == 4 {
			slug = "needle-slug"
		}
		if index == 5 {
			name = strings.Repeat("\u4e2d", 128)
		}
		if _, err := db.Exec(`INSERT INTO tenants(id,slug,name)VALUES($1,$2,$3)`, id, slug, name); err != nil {
			t.Fatal(err)
		}
	}
	found := map[string]bool{}
	for offset := 0; offset < 50; offset += 7 {
		page := readWorkspacePage(t, s, admin, fmt.Sprintf("?limit=7&offset=%d", offset))
		for _, item := range page.Items {
			if found[item.ID] {
				t.Fatalf("duplicate across pages: %s", item.ID)
			}
			found[item.ID] = true
		}
		if !page.HasMore {
			break
		}
	}
	if len(found) != len(expected) {
		t.Fatalf("pagination dropped visible rows: %d of %d", len(found), len(expected))
	}
	for id := range expected {
		if !found[id] {
			t.Fatalf("missing visible workspace %s", id)
		}
	}
	for _, sample := range []struct{ search, id string }{{"%_\\", fmt.Sprintf("%026d", 103)}, {"NEEDLE-SLUG", fmt.Sprintf("%026d", 104)}, {strings.Repeat("\u4e2d", 128), fmt.Sprintf("%026d", 105)}} {
		assertWorkspaceIDs(t, readWorkspacePage(t, s, admin, "?search="+url.QueryEscape(sample.search)), sample.id)
	}
	for _, search := range []string{"%00", "before%00after"} {
		resp := get(t, s.Client(), s.URL+"/api/v1/workspaces?search="+search, admin)
		requireStatus(t, resp, http.StatusBadRequest)
		_ = resp.Body.Close()
	}
	created := createTenant(t, s, admin, "discover-after-reload")
	assertWorkspaceIDs(t, readWorkspacePage(t, s, admin, "?search=discover-after-reload"), created)
	// The original tenant list has not become a global directory.
	resp := get(t, s.Client(), s.URL+"/api/v1/tenants", admin)
	requireStatus(t, resp, http.StatusOK)
	var old struct {
		Items []struct {
			ID string `json:"id"`
		}
	}
	decodeJSON(t, resp, &old)
	if len(old.Items) != 1 || old.Items[0].ID != bootstrap.DefaultTenantID {
		t.Fatalf("legacy list scope widened: %+v", old)
	}
}
func TestWorkspaceDirectoryOrdinaryGroupDenyBoundaryAndPlatform(t *testing.T) {
	s, db, admin := newIntegrationServer(t)
	defer s.Close()
	home := createTenant(t, s, admin, "directory-home")
	b := createTenant(t, s, admin, "directory-b")
	c := createTenant(t, s, admin, "directory-c")
	_, operator := createKey(t, s, admin, home, nil, "")
	assertWorkspaceIDs(t, readWorkspacePage(t, s, operator, "?limit=1&tenant_id="+b), home)
	assertWorkspaceIDs(t, readWorkspacePage(t, s, operator, "?search=directory-b"))
	resp := get(t, s.Client(), s.URL+"/api/v1/tenants/"+home, operator)
	requireStatus(t, resp, http.StatusForbidden)
	_ = resp.Body.Close()
	_, tenantAdmin := createKey(t, s, admin, home, []string{tenantAdminPolicyID}, "")
	assertWorkspaceIDs(t, readWorkspacePage(t, s, tenantAdmin, ""), home)
	deny := workspaceTestPolicy(t, db, home, "Hidden B", tenantReadDocument("Deny", b))
	_, boundedDeny := createKey(t, s, admin, home, []string{administratorPolicyID, deny}, "")
	result := readWorkspacePage(t, s, boundedDeny, "?search=directory-")
	assertWorkspaceIDs(t, result, home, c)
	first := readWorkspacePage(t, s, boundedDeny, "?search=directory-&limit=1")
	if len(first.Items) != 1 || first.Items[0].ID != c || !first.HasMore {
		t.Fatalf("hidden row affected first page: %+v", first)
	}
	assertWorkspaceIDs(t, readWorkspacePage(t, s, boundedDeny, "?search=directory-&limit=1&offset=1"), home)
	ceiling := workspaceTestPolicy(t, db, home, "Exact B boundary", tenantReadDocument("Allow", b))
	_, bounded := createKey(t, s, admin, home, []string{administratorPolicyID}, ceiling)
	assertWorkspaceIDs(t, readWorkspacePage(t, s, bounded, "?search=directory-"), home, b)
	// Explicit own-Get denial does not erase the existing identity-label exception.
	ownDeny := workspaceTestPolicy(t, db, home, "Deny own Get", tenantReadDocument("Deny", home))
	_, ownKey := createKey(t, s, admin, home, []string{ownDeny}, "")
	assertWorkspaceIDs(t, readWorkspacePage(t, s, ownKey, ""), home)
	resp = postJSON(t, s.Client(), s.URL+"/api/v1/tenants/"+home+"/resource_groups", admin, map[string]any{"name": "Bound group", "slug": "bound"})
	requireStatus(t, resp, http.StatusCreated)
	var group struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &group)
	resp = postJSON(t, s.Client(), s.URL+"/api/v1/tenants/"+home+"/api_keys", admin, map[string]any{"policies": []string{administratorPolicyID}, "resource_group_id": group.ID})
	requireStatus(t, resp, http.StatusCreated)
	var grouped struct {
		Key string `json:"key"`
	}
	decodeJSON(t, resp, &grouped)
	assertWorkspaceIDs(t, readWorkspacePage(t, s, grouped.Key, ""), home)
	// Platform metadata access is independent of a selected business tenant.
	preset := workspaceTestPolicy(t, db, nil, "Platform exact B", tenantReadDocument("Allow", b))
	platformID, platform := createKey(t, s, admin, home, []string{preset}, "")
	if _, err := db.Exec(`UPDATE api_keys SET kind='platform',tenant_id=NULL WHERE id=$1`, platformID); err != nil {
		t.Fatal(err)
	}
	assertWorkspaceIDs(t, readWorkspacePage(t, s, platform, ""), b)
	assertWorkspaceIDs(t, readWorkspacePage(t, s, platform, "?tenant_id="+c), b)
	emptyID, empty := createKey(t, s, admin, home, nil, "")
	if _, err := db.Exec(`UPDATE api_keys SET kind='platform',tenant_id=NULL WHERE id=$1`, emptyID); err != nil {
		t.Fatal(err)
	}
	assertWorkspaceIDs(t, readWorkspacePage(t, s, empty, ""))
}

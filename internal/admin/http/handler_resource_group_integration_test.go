//go:build integration

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createGroup makes a resource group and returns its id.
func createGroup(t *testing.T, server *httptest.Server, authKey, slug string) string {
	t.Helper()
	resp := postJSON(t, server.Client(), server.URL+"/api/v1/resource_groups", authKey, map[string]any{
		"slug": slug,
		"name": slug,
	})
	requireStatus(t, resp, http.StatusCreated)
	var out struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &out)
	return out.ID
}

// createKeyScoped mints a key scoped to one resource group.
func createKeyScoped(t *testing.T, server *httptest.Server, authKey, tenantID, groupID string) (id, key string) {
	t.Helper()
	resp := postJSON(t, server.Client(),
		fmt.Sprintf("%s/api/v1/tenants/%s/api_keys", server.URL, tenantID), authKey,
		map[string]any{
			"policies":          []string{tenantAdminPolicyID},
			"resource_group_id": groupID,
		})
	requireStatus(t, resp, http.StatusCreated)
	var out struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	decodeJSON(t, resp, &out)
	return out.ID, out.Key
}

// TestGroupScopedKeyIsRefusedOnGrouplessLists pins the group tier's semantics
// for the resources that have no group of their own. A job definition is a
// ScheduledJob Custom Resource and a run is a ledger row keyed by its
// definition; neither carries a group column, so a group-scoped caller cannot
// be narrowed -- it is refused outright. The refusal must be a 403 through the
// whole chain (auth, guard, handler, use-case gate), never a silent empty list
// that would read as "nothing in my group" and hide the scoping from the
// caller. An unscoped key of the same tenant is served, proving the refusal
// comes from the scope and not from the route.
func TestGroupScopedKeyIsRefusedOnGrouplessLists(t *testing.T) {
	server, _, bootstrapKey := newIntegrationServer(t)
	defer server.Close()

	tenantID := createTenant(t, server, bootstrapKey, "tenant-a")
	_, adminKey := createKey(t, server, bootstrapKey, tenantID, []string{tenantAdminPolicyID}, "")
	ciGroup := createGroup(t, server, adminKey, "ci")
	_, ciKey := createKeyScoped(t, server, adminKey, tenantID, ciGroup)

	requireStatus(t, get(t, server.Client(), server.URL+"/api/v1/jobs", ciKey), http.StatusForbidden)
	requireStatus(t, get(t, server.Client(), server.URL+"/api/v1/instances", ciKey), http.StatusForbidden)

	requireStatus(t, get(t, server.Client(), server.URL+"/api/v1/jobs", adminKey), http.StatusOK)
	requireStatus(t, get(t, server.Client(), server.URL+"/api/v1/instances", adminKey), http.StatusOK)
}

// TestCheckPathsRespectResourceGroup covers checks, which do carry a group: the
// column is stamped from the caller's scope on create and filtered on read.
//
// A check is created by a ci-scoped key; a prod-scoped key must not read,
// pause, resume or delete it.
func TestCheckPathsRespectResourceGroup(t *testing.T) {
	server, _, bootstrapKey := newIntegrationServer(t)
	defer server.Close()

	tenantID := createTenant(t, server, bootstrapKey, "tenant-a")
	_, adminKey := createKey(t, server, bootstrapKey, tenantID, []string{tenantAdminPolicyID}, "")
	ciGroup := createGroup(t, server, adminKey, "ci")
	prodGroup := createGroup(t, server, adminKey, "prod")

	_, ciKey := createKeyScoped(t, server, adminKey, tenantID, ciGroup)
	_, prodKey := createKeyScoped(t, server, adminKey, tenantID, prodGroup)

	resp := postJSON(t, server.Client(), server.URL+"/api/v1/checks", ciKey, map[string]any{
		"name":            "ci-health",
		"check_type":      "http_health",
		"check_config":    map[string]any{"url": "http://localhost/healthz"},
		"assertion_rules": []map[string]any{{"metric": "status_code", "operator": "==", "threshold": 200, "severity": "warning"}},
		"cron_expr":       "*/5 * * * *",
	})
	requireStatus(t, resp, http.StatusCreated)
	var check struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, resp, &check)

	base := fmt.Sprintf("%s/api/v1/checks/%d", server.URL, check.ID)
	requireStatus(t, get(t, server.Client(), base, ciKey), http.StatusOK)
	requireStatus(t, get(t, server.Client(), base, prodKey), http.StatusNotFound)

	paused := postJSON(t, server.Client(), base+"/pause", prodKey, map[string]any{"version": 1})
	requireStatus(t, paused, http.StatusNotFound)
	_ = paused.Body.Close()

	deleted := deleteReq(t, server.Client(), base, prodKey)
	requireStatus(t, deleted, http.StatusNotFound)
	_ = deleted.Body.Close()

	// The check is untouched.
	after := get(t, server.Client(), base, ciKey)
	requireStatus(t, after, http.StatusOK)
	var state struct {
		Status  string `json:"status"`
		Version int    `json:"version"`
	}
	decodeJSON(t, after, &state)
	if state.Status != "active" || state.Version != 1 {
		t.Fatalf("the check was modified by a key outside its group: %+v", state)
	}
}

// TestSLOPathsRespectResourceGroup covers the same isolation for SLIs and SLOs,
// which have their own get, pause and delete routes.
func TestSLOPathsRespectResourceGroup(t *testing.T) {
	server, _, bootstrapKey := newIntegrationServer(t)
	defer server.Close()

	tenantID := createTenant(t, server, bootstrapKey, "tenant-a")
	_, adminKey := createKey(t, server, bootstrapKey, tenantID, []string{tenantAdminPolicyID}, "")
	ciGroup := createGroup(t, server, adminKey, "ci")
	prodGroup := createGroup(t, server, adminKey, "prod")

	_, ciKey := createKeyScoped(t, server, adminKey, tenantID, ciGroup)
	_, prodKey := createKeyScoped(t, server, adminKey, tenantID, prodGroup)

	// An SLI and an SLO to hang off it, both created inside the ci group. The
	// SLI's source is the run ledger: source_type job_run names its definition
	// by source_uid in source_config.
	sliResp := postJSON(t, server.Client(), server.URL+"/api/v1/slis", ciKey, map[string]any{
		"name":        "ci-availability",
		"sli_type":    "availability",
		"source_type": "job_run",
		"source_config": map[string]any{
			"source_uid": "01JSLI00000000000000000001",
		},
		"aggregation":         "ratio",
		"good_event_criteria": map[string]any{"status": "success"},
	})
	requireStatus(t, sliResp, http.StatusCreated)
	var sli struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, sliResp, &sli)

	sloResp := postJSON(t, server.Client(), server.URL+"/api/v1/slos", ciKey, map[string]any{
		"name":            "ci-slo",
		"sli_id":          sli.ID,
		"target":          0.999,
		"window_type":     "rolling",
		"window_duration": "24h",
	})
	requireStatus(t, sloResp, http.StatusCreated)
	var slo struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, sloResp, &slo)

	sliBase := fmt.Sprintf("%s/api/v1/slis/%d", server.URL, sli.ID)
	sloBase := fmt.Sprintf("%s/api/v1/slos/%d", server.URL, slo.ID)

	requireStatus(t, get(t, server.Client(), sliBase, ciKey), http.StatusOK)
	requireStatus(t, get(t, server.Client(), sliBase, prodKey), http.StatusNotFound)
	requireStatus(t, get(t, server.Client(), sloBase, ciKey), http.StatusOK)
	requireStatus(t, get(t, server.Client(), sloBase, prodKey), http.StatusNotFound)

	sloPaused := postJSON(t, server.Client(), sloBase+"/pause", prodKey, map[string]any{"version": 1})
	requireStatus(t, sloPaused, http.StatusNotFound)
	_ = sloPaused.Body.Close()

	sloDeleted := deleteReq(t, server.Client(), sloBase, prodKey)
	requireStatus(t, sloDeleted, http.StatusNotFound)
	_ = sloDeleted.Body.Close()

	// A nonexistent SLO gives the same answer, so the response cannot be used
	// to probe for ids.
	missing := get(t, server.Client(), server.URL+"/api/v1/slos/999999", ciKey)
	requireStatus(t, missing, http.StatusNotFound)
	_ = missing.Body.Close()

	sliDeleted := deleteReq(t, server.Client(), sliBase, prodKey)
	requireStatus(t, sliDeleted, http.StatusNotFound)
	_ = sliDeleted.Body.Close()

	// Both survive.
	requireStatus(t, get(t, server.Client(), sliBase, ciKey), http.StatusOK)
	requireStatus(t, get(t, server.Client(), sloBase, ciKey), http.StatusOK)
}

// TestCheckCreateRecordsResourceGroup checks the other half of the check
// isolation: the column is populated on create. A filter over a column nobody
// writes would return nothing for every scoped key -- an outage that looks like
// a permissions bug. The job create path this assertion originally covered is
// gone with the write API; checks are the surviving stamped surface.
func TestCheckCreateRecordsResourceGroup(t *testing.T) {
	server, db, bootstrapKey := newIntegrationServer(t)
	defer server.Close()

	tenantID := createTenant(t, server, bootstrapKey, "tenant-a")
	_, adminKey := createKey(t, server, bootstrapKey, tenantID, []string{tenantAdminPolicyID}, "")
	ciGroup := createGroup(t, server, adminKey, "ci")
	_, ciKey := createKeyScoped(t, server, adminKey, tenantID, ciGroup)

	resp := postJSON(t, server.Client(), server.URL+"/api/v1/checks", ciKey, map[string]any{
		"name":            "ci-health",
		"check_type":      "http_health",
		"check_config":    map[string]any{"url": "http://localhost/healthz"},
		"assertion_rules": []map[string]any{{"metric": "status_code", "operator": "==", "threshold": 200, "severity": "warning"}},
		"cron_expr":       "*/5 * * * *",
	})
	requireStatus(t, resp, http.StatusCreated)
	var check struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, resp, &check)

	var groupID *string
	if err := db.QueryRowContext(context.Background(),
		"SELECT resource_group_id FROM checks WHERE id = $1", check.ID).Scan(&groupID); err != nil {
		t.Fatalf("read check resource group: %v", err)
	}
	if groupID == nil || *groupID != ciGroup {
		t.Fatalf("expected check group %s, got %v", ciGroup, groupID)
	}
}

// deleteReq issues the DELETE these routes expect: a version in the body.
func deleteReq(t *testing.T, client *http.Client, url, authKey string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]any{"version": 1})
	if err != nil {
		t.Fatalf("marshal delete body: %v", err)
	}
	req, err := http.NewRequest(http.MethodDelete, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create delete request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authKey != "" {
		req.Header.Set("Authorization", "Bearer "+authKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do delete request: %v", err)
	}
	return resp
}

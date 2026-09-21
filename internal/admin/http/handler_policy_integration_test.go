//go:build integration

package http

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The three preset policies seeded by the baseline. Tests bind them rather
// than defining one per case: a preset is what a real caller starts from, and
// its exact contents are already asserted by the migration tests.
const (
	administratorPolicyID = "00000000000000000000000010"
	// tenantAdminPolicyID is the platform TenantAdminAccess preset. It grants
	// the tenant-facing actions a tenant administrator needs, including
	// apikey:Create and job:Trigger, and deliberately excludes anything
	// platform-scoped.
	tenantAdminPolicyID = "00000000000000000000000011"
	readOnlyPolicyID    = "00000000000000000000000012"
)

// createTenant makes a tenant and returns its id.
func createTenant(t *testing.T, server *httptest.Server, bootstrapKey, slug string) string {
	t.Helper()
	resp := postJSON(t, server.Client(), server.URL+"/api/v1/tenants", bootstrapKey, map[string]any{
		"slug":   slug,
		"name":   slug,
		"status": "active",
	})
	requireStatus(t, resp, http.StatusCreated)
	var tenant struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &tenant)
	if tenant.ID == "" {
		t.Fatal("tenant id missing from response")
	}
	return tenant.ID
}

// createKey mints a key for tenantID and returns its id and plaintext.
//
// The plaintext is only ever available here: the API returns it once, and the
// stored row holds a hash.
func createKey(t *testing.T, server *httptest.Server, authKey, tenantID string,
	policies []string, boundary string) (id, key string) {
	t.Helper()
	body := map[string]any{}
	if policies != nil {
		body["policies"] = policies
	}
	if boundary != "" {
		body["boundary_policy_id"] = boundary
	}
	resp := postJSON(t, server.Client(),
		fmt.Sprintf("%s/api/v1/tenants/%s/api_keys", server.URL, tenantID), authKey, body)
	requireStatus(t, resp, http.StatusCreated)
	var out struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	decodeJSON(t, resp, &out)
	if out.Key == "" {
		t.Fatal("api key missing from response")
	}
	return out.ID, out.Key
}

// TestCreateAPIKey_RejectsEscalation is the guard that makes self-service
// safe. A key holding TenantAdminAccess may create keys, but it must not be
// able to create one carrying AdministratorAccess: delegating a permission it
// does not hold would let any tenant administrator mint itself a platform
// administrator, one request at a time.
func TestCreateAPIKey_RejectsEscalation(t *testing.T) {
	server, _, bootstrapKey := newIntegrationServer(t)
	defer server.Close()

	tenantID := createTenant(t, server, bootstrapKey, "tenant-a")
	// Bounded by the same policy it holds, so it can create keys but has a
	// ceiling to escape from.
	_, keyA := createKey(t, server, bootstrapKey, tenantID,
		[]string{tenantAdminPolicyID}, tenantAdminPolicyID)

	resp := postJSON(t, server.Client(),
		fmt.Sprintf("%s/api/v1/tenants/%s/api_keys", server.URL, tenantID), keyA,
		map[string]any{"policies": []string{administratorPolicyID}})
	requireStatus(t, resp, http.StatusForbidden)
	_ = resp.Body.Close()
}

// TestCreateAPIKey_PropagatesBoundary covers the AWS gotcha the design
// documents: a boundary does not automatically apply to keys its holder
// creates. If it did not propagate, a bounded key could create an unbounded
// key and act through it.
func TestCreateAPIKey_PropagatesBoundary(t *testing.T) {
	server, db, bootstrapKey := newIntegrationServer(t)
	defer server.Close()

	tenantID := createTenant(t, server, bootstrapKey, "tenant-a")
	_, keyA := createKey(t, server, bootstrapKey, tenantID,
		[]string{tenantAdminPolicyID}, tenantAdminPolicyID)

	// keyA names no boundary for keyB. It must inherit keyA's.
	keyBID, _ := createKey(t, server, keyA, tenantID, []string{tenantAdminPolicyID}, "")

	var boundary sql.NullString
	if err := db.QueryRowContext(context.Background(),
		`SELECT boundary_policy_id FROM api_keys WHERE id = $1`, keyBID).Scan(&boundary); err != nil {
		t.Fatalf("query boundary: %v", err)
	}
	if !boundary.Valid || boundary.String != tenantAdminPolicyID {
		t.Fatalf("expected keyB to inherit boundary %s, got %v", tenantAdminPolicyID, boundary)
	}
}

// TestCreateAPIKey_RejectsWiderBoundary covers the other direction: naming a
// boundary wider than the caller's own. Propagation would otherwise be
// decorative, since the caller could simply ask for a bigger ceiling.
func TestCreateAPIKey_RejectsWiderBoundary(t *testing.T) {
	server, _, bootstrapKey := newIntegrationServer(t)
	defer server.Close()

	tenantID := createTenant(t, server, bootstrapKey, "tenant-a")
	_, keyA := createKey(t, server, bootstrapKey, tenantID,
		[]string{tenantAdminPolicyID}, tenantAdminPolicyID)

	resp := postJSON(t, server.Client(),
		fmt.Sprintf("%s/api/v1/tenants/%s/api_keys", server.URL, tenantID), keyA,
		map[string]any{
			"policies":           []string{tenantAdminPolicyID},
			"boundary_policy_id": administratorPolicyID,
		})
	requireStatus(t, resp, http.StatusForbidden)
	_ = resp.Body.Close()
}

// A key created by an unbounded key of the same tenant may not silently
// acquire grants the creator lacks, even when the creator is unrestricted.
func TestCreateAPIKey_RejectsPolicyTheCallerDoesNotHold(t *testing.T) {
	server, _, bootstrapKey := newIntegrationServer(t)
	defer server.Close()

	tenantID := createTenant(t, server, bootstrapKey, "tenant-a")
	// ReadOnlyAccess grants no apikey:Create, so this key cannot even reach the
	// route. The refusal must still be a 403 and not a 500 or a silent success.
	_, readOnlyKey := createKey(t, server, bootstrapKey, tenantID, []string{readOnlyPolicyID}, "")

	resp := postJSON(t, server.Client(),
		fmt.Sprintf("%s/api/v1/tenants/%s/api_keys", server.URL, tenantID), readOnlyKey,
		map[string]any{"policies": []string{readOnlyPolicyID}})
	requireStatus(t, resp, http.StatusForbidden)
	_ = resp.Body.Close()
}

// TestCreateAPIKey_WritesAuditEvent checks that the grant is recorded with its
// contents. "Who can create bindings" is itself an escalation vector, so the
// answer has to survive the fact: the row must name the acting key, the new
// key, and what it was given.
func TestCreateAPIKey_WritesAuditEvent(t *testing.T) {
	server, db, bootstrapKey := newIntegrationServer(t)
	defer server.Close()

	tenantID := createTenant(t, server, bootstrapKey, "tenant-a")
	keyAID, keyA := createKey(t, server, bootstrapKey, tenantID,
		[]string{tenantAdminPolicyID}, tenantAdminPolicyID)

	keyBID, _ := createKey(t, server, keyA, tenantID, []string{tenantAdminPolicyID}, "")

	var (
		actorID      string
		resourceID   string
		eventType    string
		resourceType string
		diff         string
	)
	err := db.QueryRowContext(context.Background(), `
		SELECT actor_id, resource_id, event_type, resource_type, diff::text
		FROM audit_events
		WHERE resource_type = 'apikey' AND resource_id = $1
	`, keyBID).Scan(&actorID, &resourceID, &eventType, &resourceType, &diff)
	if err != nil {
		t.Fatalf("read audit event for keyB: %v", err)
	}
	if actorID != keyAID {
		t.Fatalf("expected actor %s, got %s", keyAID, actorID)
	}
	if eventType != "create" || resourceType != "apikey" {
		t.Fatalf("unexpected event %s/%s", resourceType, eventType)
	}
	// The diff must carry the boundary that was actually applied, not the one
	// that was requested -- here keyB inherited it rather than naming it.
	if !containsAll(diff, tenantAdminPolicyID, "boundary_policy_id") {
		t.Fatalf("audit diff does not record the applied grant: %s", diff)
	}
}

// containsAll reports whether every needle appears in haystack. The diff is
// JSON whose key order is not guaranteed, so it is searched rather than parsed
// into a shape the test would then have to keep in step with the writer.
func containsAll(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}

// TestCreatePolicy_RejectsWildcard covers security finding 1-4. A tenant that
// can author policies and create keys can reach any action it can name, so a
// tenant-authored document must not be able to name every action at once:
// that is the escalation the subset guard exists to stop, arriving through the
// front door instead.
func TestCreatePolicy_RejectsWildcard(t *testing.T) {
	server, _, bootstrapKey := newIntegrationServer(t)
	defer server.Close()

	tenantID := createTenant(t, server, bootstrapKey, "tenant-a")
	_, key := createKey(t, server, bootstrapKey, tenantID, []string{tenantAdminPolicyID}, "")

	resp := postJSON(t, server.Client(), server.URL+"/api/v1/policies", key, map[string]any{
		"name": "Everything",
		"document": map[string]any{
			"version": "1",
			"statement": []map[string]any{{
				"effect":   "Allow",
				"action":   []string{"*"},
				"resource": []string{"orbitjob:self:*:*/*"},
			}},
		},
	})
	requireStatus(t, resp, http.StatusBadRequest)
	_ = resp.Body.Close()
}

// A misspelled action must be refused at creation. Accepted silently, it reads
// as a working grant, and the next move is to widen it until something
// happens -- which is how a typo becomes an over-grant.
func TestCreatePolicy_RejectsUnknownAction(t *testing.T) {
	server, _, bootstrapKey := newIntegrationServer(t)
	defer server.Close()

	tenantID := createTenant(t, server, bootstrapKey, "tenant-a")
	_, key := createKey(t, server, bootstrapKey, tenantID, []string{tenantAdminPolicyID}, "")

	resp := postJSON(t, server.Client(), server.URL+"/api/v1/policies", key, map[string]any{
		"name": "Typo",
		"document": map[string]any{
			"version": "1",
			"statement": []map[string]any{{
				"effect":   "Allow",
				"action":   []string{"job:Creat"},
				"resource": []string{"orbitjob:self:*:job/*"},
			}},
		},
	})
	requireStatus(t, resp, http.StatusBadRequest)
	_ = resp.Body.Close()
}

// TestPolicyLifecycle covers the round trip: a valid tenant policy is created,
// listed, read back and deleted, and the delete is recorded.
func TestPolicyLifecycle(t *testing.T) {
	server, db, bootstrapKey := newIntegrationServer(t)
	defer server.Close()

	tenantID := createTenant(t, server, bootstrapKey, "tenant-a")
	_, key := createKey(t, server, bootstrapKey, tenantID, []string{tenantAdminPolicyID}, "")

	resp := postJSON(t, server.Client(), server.URL+"/api/v1/policies", key, map[string]any{
		"name":        "JobOperator",
		"description": "Runs jobs, changes nothing else",
		"document": map[string]any{
			"version": "1",
			"statement": []map[string]any{{
				"effect":   "Allow",
				"action":   []string{"job:Get", "job:List", "job:Trigger"},
				"resource": []string{"orbitjob:self:*:job/*"},
			}},
		},
	})
	requireStatus(t, resp, http.StatusCreated)
	var created struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &created)

	// List must show it alongside the presets, marked as not-platform so a
	// caller can tell which ones it may delete.
	resp = get(t, server.Client(), server.URL+"/api/v1/policies", key)
	requireStatus(t, resp, http.StatusOK)
	var list struct {
		Items []struct {
			ID       string `json:"id"`
			Platform bool   `json:"platform"`
		} `json:"items"`
	}
	decodeJSON(t, resp, &list)
	var found, sawPreset bool
	for _, item := range list.Items {
		if item.ID == created.ID {
			found = true
			if item.Platform {
				t.Error("a tenant-authored policy was reported as a platform preset")
			}
		}
		if item.Platform {
			sawPreset = true
		}
	}
	if !found {
		t.Fatal("created policy missing from the list")
	}
	if !sawPreset {
		t.Fatal("platform presets missing from the list; a tenant could not bind one")
	}

	resp = get(t, server.Client(), server.URL+"/api/v1/policies/"+created.ID, key)
	requireStatus(t, resp, http.StatusOK)
	_ = resp.Body.Close()

	// Delete is a DELETE, so it is issued directly rather than through postJSON.
	req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/policies/"+created.ID, nil)
	if err != nil {
		t.Fatalf("build delete request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	delResp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("delete policy: %v", err)
	}
	requireStatus(t, delResp, http.StatusNoContent)
	_ = delResp.Body.Close()

	var auditCount int
	if err := db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM audit_events
		WHERE resource_type = 'policy' AND resource_id = $1 AND event_type = 'delete'
	`, created.ID).Scan(&auditCount); err != nil {
		t.Fatalf("count delete audit events: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 delete audit event, got %d", auditCount)
	}
}

// A platform preset belongs to the installation. Editing one would change what
// every tenant that already holds it can do, so deletion is refused for every
// caller -- including the one holding AdministratorAccess.
func TestDeletePolicy_RejectsPlatformPreset(t *testing.T) {
	server, _, bootstrapKey := newIntegrationServer(t)
	defer server.Close()

	req, err := http.NewRequest(http.MethodDelete,
		server.URL+"/api/v1/policies/"+readOnlyPolicyID, nil)
	if err != nil {
		t.Fatalf("build delete request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bootstrapKey)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("delete preset: %v", err)
	}
	requireStatus(t, resp, http.StatusForbidden)
	_ = resp.Body.Close()
}

// TestResourceGroupLifecycle covers group creation and listing. The slug is the
// segment that appears in an ARN, so it is validated rather than accepted raw.
func TestResourceGroupLifecycle(t *testing.T) {
	server, _, bootstrapKey := newIntegrationServer(t)
	defer server.Close()

	tenantID := createTenant(t, server, bootstrapKey, "tenant-a")
	_, key := createKey(t, server, bootstrapKey, tenantID, []string{tenantAdminPolicyID}, "")

	resp := postJSON(t, server.Client(), server.URL+"/api/v1/resource_groups", key, map[string]any{
		"slug": "ci",
		"name": "Continuous Integration",
	})
	requireStatus(t, resp, http.StatusCreated)
	_ = resp.Body.Close()

	resp = get(t, server.Client(), server.URL+"/api/v1/resource_groups", key)
	requireStatus(t, resp, http.StatusOK)
	var list struct {
		Items []struct {
			Slug string `json:"slug"`
		} `json:"items"`
	}
	decodeJSON(t, resp, &list)
	if len(list.Items) != 1 || list.Items[0].Slug != "ci" {
		t.Fatalf("unexpected group list: %+v", list.Items)
	}

	// A slug colliding with an existing one is a conflict, not a second group.
	resp = postJSON(t, server.Client(), server.URL+"/api/v1/resource_groups", key, map[string]any{
		"slug": "ci",
		"name": "Duplicate",
	})
	requireStatus(t, resp, http.StatusConflict)
	_ = resp.Body.Close()
}

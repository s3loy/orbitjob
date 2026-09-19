//go:build integration

package http

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"orbitjob/internal/admin/bootstrap"
)

// The three tests in this file pin API key tenant isolation. The two negative
// probes were written against the job create route; with definitions declared
// as Custom Resources outside this API, the fixture seeds a revision row the
// way the operator would, and the probe is the read-only job route. The
// positive test uses policies, a tenant-scoped surface a key can still create
// and read.
//
// In each case the key carries TenantAdminAccess, which resolves "self"
// against its own tenant. A key with no policies can authenticate but reach
// nothing, which is the intended default rather than a test convenience.

// seedDefinition inserts one active revision for tenantID, standing in for the
// projection the operator writes when a ScheduledJob is applied.
func seedDefinition(t *testing.T, db *sql.DB, tenantID, sourceUID, name string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO job_definition_revisions
		  (tenant_id, source_mode, source_uid, source_namespace, source_name,
		   generation, spec_hash, normalized_spec, is_active, actor)
		VALUES ($1, 'kubernetes', $2, 'orbitjob-system', $3, 1, $4, '{}'::jsonb, true, 'integration-test')
		RETURNING id
	`, tenantID, sourceUID, name, strings.Repeat("a", 64)).Scan(&id)
	if err != nil {
		t.Fatalf("seed definition for tenant %s: %v", tenantID, err)
	}
	return id
}

func TestAPIKey_TenantKeyAccessesOwnResources(t *testing.T) {
	server, db, bootstrapKey := newIntegrationServer(t)
	defer server.Close()
	client := server.Client()

	// Create tenant A using the bootstrap key.
	resp := postJSON(t, client, server.URL+"/api/v1/tenants", bootstrapKey, map[string]any{
		"slug":   "tenant-a",
		"name":   "Tenant A",
		"status": "active",
	})
	requireStatus(t, resp, http.StatusCreated)
	var tenant struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &tenant)

	// Create an API key for tenant A.
	resp = postJSON(t, client, fmt.Sprintf("%s/api/v1/tenants/%s/api_keys", server.URL, tenant.ID), bootstrapKey, map[string]any{
		"policies": []string{tenantAdminPolicyID},
	})
	requireStatus(t, resp, http.StatusCreated)
	var key struct {
		Key string `json:"key"`
	}
	decodeJSON(t, resp, &key)
	if key.Key == "" {
		t.Fatal("expected api key in response")
	}

	// Author a policy as tenant A.
	resp = postJSON(t, client, server.URL+"/api/v1/policies", key.Key, map[string]any{
		"name": "tenant-a-policy",
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
	var policy struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &policy)

	// Tenant A's key can read its own policy.
	resp = get(t, client, fmt.Sprintf("%s/api/v1/policies/%s", server.URL, policy.ID), key.Key)
	requireStatus(t, resp, http.StatusOK)

	// Verify the policy belongs to tenant A.
	var policyTenantID string
	err := db.QueryRowContext(context.Background(), "SELECT tenant_id FROM policies WHERE id = $1", policy.ID).Scan(&policyTenantID)
	if err != nil {
		t.Fatalf("query policy tenant: %v", err)
	}
	if policyTenantID != tenant.ID {
		t.Fatalf("expected policy tenant %s, got %s", tenant.ID, policyTenantID)
	}
}

func TestAPIKey_TenantKeyCannotAccessOtherTenant(t *testing.T) {
	server, db, bootstrapKey := newIntegrationServer(t)
	defer server.Close()
	client := server.Client()

	// A definition owned by the default/bootstrap tenant. It exists: the 404
	// below is isolation, not absence.
	seedDefinition(t, db, bootstrap.DefaultTenantID, "01JDEF00000000000000000000", "default-tenant-job")

	// Create tenant A and its key.
	resp := postJSON(t, client, server.URL+"/api/v1/tenants", bootstrapKey, map[string]any{
		"slug":   "tenant-a",
		"name":   "Tenant A",
		"status": "active",
	})
	requireStatus(t, resp, http.StatusCreated)
	var tenantA struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &tenantA)

	resp = postJSON(t, client, fmt.Sprintf("%s/api/v1/tenants/%s/api_keys", server.URL, tenantA.ID), bootstrapKey, map[string]any{
		"policies": []string{tenantAdminPolicyID},
	})
	requireStatus(t, resp, http.StatusCreated)
	var keyA struct {
		Key string `json:"key"`
	}
	decodeJSON(t, resp, &keyA)

	// Tenant A's key cannot see the default tenant's job.
	resp = get(t, client, fmt.Sprintf("%s/api/v1/jobs/%d", server.URL, 1), keyA.Key)
	requireStatus(t, resp, http.StatusNotFound)
}

func TestAPIKey_BootstrapKeyCannotAccessTenantAJob(t *testing.T) {
	server, db, bootstrapKey := newIntegrationServer(t)
	defer server.Close()
	client := server.Client()

	// Create tenant A and its key.
	resp := postJSON(t, client, server.URL+"/api/v1/tenants", bootstrapKey, map[string]any{
		"slug":   "tenant-a",
		"name":   "Tenant A",
		"status": "active",
	})
	requireStatus(t, resp, http.StatusCreated)
	var tenantA struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &tenantA)

	// A definition owned by tenant A, so the 404 below is isolation, not
	// absence.
	jobID := seedDefinition(t, db, tenantA.ID, "01JTNT00000000000000000001", "tenant-a-job")

	// Bootstrap key (default tenant) cannot see tenant A's job.
	resp = get(t, client, fmt.Sprintf("%s/api/v1/jobs/%d", server.URL, jobID), bootstrapKey)
	requireStatus(t, resp, http.StatusNotFound)
}

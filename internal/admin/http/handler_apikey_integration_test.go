//go:build integration

package http

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

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
	resp = postJSON(t, client, fmt.Sprintf("%s/api/v1/tenants/%s/api_keys", server.URL, tenant.ID), bootstrapKey, map[string]any{})
	requireStatus(t, resp, http.StatusCreated)
	var key struct {
		Key string `json:"key"`
	}
	decodeJSON(t, resp, &key)
	if key.Key == "" {
		t.Fatal("expected api key in response")
	}

	// Create a job as tenant A.
	resp = postJSON(t, client, server.URL+"/api/v1/jobs", key.Key, map[string]any{
		"name":            "tenant-a-job",
		"trigger_type":    "manual",
		"handler_type":    "http",
		"handler_payload": map[string]any{"url": "https://example.com/hook"},
	})
	requireStatus(t, resp, http.StatusCreated)
	var job struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, resp, &job)

	// Tenant A's key can read its own job.
	resp = get(t, client, fmt.Sprintf("%s/api/v1/jobs/%d", server.URL, job.ID), key.Key)
	requireStatus(t, resp, http.StatusOK)

	// Verify the job belongs to tenant A.
	var jobTenantID string
	err := db.QueryRowContext(context.Background(), "SELECT tenant_id FROM jobs WHERE id = $1", job.ID).Scan(&jobTenantID)
	if err != nil {
		t.Fatalf("query job tenant: %v", err)
	}
	if jobTenantID != tenant.ID {
		t.Fatalf("expected job tenant %s, got %s", tenant.ID, jobTenantID)
	}
}

func TestAPIKey_TenantKeyCannotAccessOtherTenant(t *testing.T) {
	server, _, bootstrapKey := newIntegrationServer(t)
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

	resp = postJSON(t, client, fmt.Sprintf("%s/api/v1/tenants/%s/api_keys", server.URL, tenantA.ID), bootstrapKey, map[string]any{})
	requireStatus(t, resp, http.StatusCreated)
	var keyA struct {
		Key string `json:"key"`
	}
	decodeJSON(t, resp, &keyA)

	// Create a job as the default/bootstrap tenant.
	resp = postJSON(t, client, server.URL+"/api/v1/jobs", bootstrapKey, map[string]any{
		"name":            "default-tenant-job",
		"trigger_type":    "manual",
		"handler_type":    "http",
		"handler_payload": map[string]any{"url": "https://example.com/hook"},
	})
	requireStatus(t, resp, http.StatusCreated)
	var defaultJob struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, resp, &defaultJob)

	// Tenant A's key cannot see the default tenant's job.
	resp = get(t, client, fmt.Sprintf("%s/api/v1/jobs/%d", server.URL, defaultJob.ID), keyA.Key)
	requireStatus(t, resp, http.StatusNotFound)
}

func TestAPIKey_BootstrapKeyCannotAccessTenantAJob(t *testing.T) {
	server, _, bootstrapKey := newIntegrationServer(t)
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

	resp = postJSON(t, client, fmt.Sprintf("%s/api/v1/tenants/%s/api_keys", server.URL, tenantA.ID), bootstrapKey, map[string]any{})
	requireStatus(t, resp, http.StatusCreated)
	var keyA struct {
		Key string `json:"key"`
	}
	decodeJSON(t, resp, &keyA)

	// Create a job as tenant A.
	resp = postJSON(t, client, server.URL+"/api/v1/jobs", keyA.Key, map[string]any{
		"name":            "tenant-a-job",
		"trigger_type":    "manual",
		"handler_type":    "http",
		"handler_payload": map[string]any{"url": "https://example.com/hook"},
	})
	requireStatus(t, resp, http.StatusCreated)
	var job struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, resp, &job)

	// Bootstrap key (default tenant) cannot see tenant A's job.
	resp = get(t, client, fmt.Sprintf("%s/api/v1/jobs/%d", server.URL, job.ID), bootstrapKey)
	requireStatus(t, resp, http.StatusNotFound)
}

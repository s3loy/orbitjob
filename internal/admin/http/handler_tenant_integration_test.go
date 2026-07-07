//go:build integration

package http

import (
	"context"
	"net/http"
	"testing"
)

func TestCreateTenant_BootstrapKeyCreatesTenant(t *testing.T) {
	server, db, bootstrapKey := newIntegrationServer(t)
	defer server.Close()
	client := server.Client()

	resp := postJSON(t, client, server.URL+"/api/v1/tenants", bootstrapKey, map[string]any{
		"slug":   "tenant-a",
		"name":   "Tenant A",
		"status": "active",
	})
	requireStatus(t, resp, http.StatusCreated)

	var body struct {
		ID     string `json:"id"`
		Slug   string `json:"slug"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	decodeJSON(t, resp, &body)
	if body.ID == "" {
		t.Fatal("expected tenant id in response")
	}
	if body.Slug != "tenant-a" {
		t.Fatalf("expected slug tenant-a, got %s", body.Slug)
	}
	if body.Name != "Tenant A" {
		t.Fatalf("expected name Tenant A, got %s", body.Name)
	}
	if body.Status != "active" {
		t.Fatalf("expected status active, got %s", body.Status)
	}

	var dbSlug string
	err := db.QueryRowContext(context.Background(), "SELECT slug FROM tenants WHERE id = $1", body.ID).Scan(&dbSlug)
	if err != nil {
		t.Fatalf("query created tenant: %v", err)
	}
	if dbSlug != "tenant-a" {
		t.Fatalf("expected db slug tenant-a, got %s", dbSlug)
	}
}

func TestCreateTenant_UnauthorizedWithoutKey(t *testing.T) {
	server, _, _ := newIntegrationServer(t)
	defer server.Close()
	client := server.Client()

	resp := postJSON(t, client, server.URL+"/api/v1/tenants", "", map[string]any{
		"slug": "tenant-a",
		"name": "Tenant A",
	})
	requireStatus(t, resp, http.StatusUnauthorized)
}

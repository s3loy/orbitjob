//go:build integration

package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	apikeycommand "orbitjob/internal/admin/app/apikey/command"
	apikeyquery "orbitjob/internal/admin/app/apikey/query"
	jobcommand "orbitjob/internal/admin/app/job/command"
	jobquery "orbitjob/internal/admin/app/job/query"
	tenantcommand "orbitjob/internal/admin/app/tenant/command"
	"orbitjob/internal/admin/bootstrap"
	"orbitjob/internal/admin/http/middleware"
	adminpostgres "orbitjob/internal/admin/store/postgres"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/postgrestest"
)

func newIntegrationServer(t *testing.T) (*httptest.Server, *sql.DB, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := postgrestest.Open(t)

	// Bootstrap default tenant and key so the auth middleware has credentials.
	bootstrapKey := "otj_integration_bootstrap_key"
	if _, err := bootstrap.EnsureDefault(context.Background(), db, bootstrap.Options{APIKey: bootstrapKey}); err != nil {
		t.Fatalf("bootstrap default tenant: %v", err)
	}

	coreJobRepo := corepostgres.NewJobRepository(db)
	adminJobRepo := adminpostgres.NewJobRepository(db)
	createJobUC := jobcommand.NewCreateJobUseCase(coreJobRepo, adminJobRepo)
	listJobsUC := jobquery.NewListJobsUseCase(adminJobRepo)
	getJobUC := jobquery.NewGetJobUseCase(adminJobRepo)
	updateJobUC := jobcommand.NewUpdateJobUseCase(coreJobRepo)
	changeStatusUC := jobcommand.NewChangeStatusUseCase(adminJobRepo, coreJobRepo)

	tenantRepo := adminpostgres.NewTenantRepository(db)
	createTenantUC := tenantcommand.NewCreator(tenantRepo)

	apiKeyRepo := adminpostgres.NewAPIKeyRepository(db)
	createAPIKeyUC := apikeycommand.NewCreator(apiKeyRepo)
	listAPIKeysUC := apikeyquery.NewLister(apiKeyRepo)

	handler := NewHandler(createJobUC, listJobsUC, getJobUC, updateJobUC, changeStatusUC)
	handler.SetCreateTenantUseCase(createTenantUC)
	handler.SetCreateAPIKeyUseCase(createAPIKeyUC)
	handler.SetListAPIKeysUseCase(listAPIKeysUC)

	auth := middleware.NewAuth(db)

	r := gin.Default()
	r.Use(auth.Middleware())
	handler.Register(r)

	return httptest.NewServer(r), db, bootstrapKey
}

func postJSON(t *testing.T, client *http.Client, url, authKey string, body any) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, url, bodyReader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authKey != "" {
		req.Header.Set("Authorization", "Bearer "+authKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func get(t *testing.T, client *http.Client, url, authKey string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if authKey != "" {
		req.Header.Set("Authorization", "Bearer "+authKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func requireStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("expected status %d, got %d: %s", want, resp.StatusCode, string(body))
	}
}

func decodeJSON(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("decode response: %v (body: %s)", err, string(body))
	}
}

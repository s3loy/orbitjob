package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adminhttp "orbitjob/internal/admin/http"

	"github.com/gin-gonic/gin"
)

func TestNewRouter_OpenAPIRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := adminhttp.NewHandler(
		&stubCreateJobUseCase{},
		&stubListJobsUseCase{},
		&stubGetJobUseCase{},
		&stubUpdateJobUseCase{},
		&stubChangeStatusUseCase{},
	)
	router := newRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}

	var doc struct {
		OpenAPI string                     `json:"openapi"`
		Paths   map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if doc.OpenAPI != "3.0.3" {
		t.Fatalf("expected openapi version=%q, got %q", "3.0.3", doc.OpenAPI)
	}
	if _, ok := doc.Paths["/healthz"]; !ok {
		t.Fatalf("expected /healthz path, got %+v", doc.Paths)
	}
	if _, ok := doc.Paths["/metrics"]; !ok {
		t.Fatalf("expected /metrics path, got %+v", doc.Paths)
	}
	if _, ok := doc.Paths["/openapi.json"]; !ok {
		t.Fatalf("expected /openapi.json path, got %+v", doc.Paths)
	}
	if _, ok := doc.Paths["/api/v1/jobs"]; !ok {
		t.Fatalf("expected /api/v1/jobs path, got %+v", doc.Paths)
	}
}

func TestNewRouter_OpenAPIRoute_WithNilHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := newRouter(nil)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}

	var doc struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if _, ok := doc.Paths["/api/v1/jobs"]; !ok {
		t.Fatalf("expected service OpenAPI to include /api/v1/jobs even with nil handler, got %+v", doc.Paths)
	}
}

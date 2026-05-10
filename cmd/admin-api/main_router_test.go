package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	adminhttp "orbitjob/internal/admin/http"
	"orbitjob/internal/admin/http/middleware"
)

func TestNewRouter_WithAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	handler := adminhttp.NewHandler(
		&stubCreateJobUseCase{},
		&stubListJobsUseCase{},
		&stubGetJobUseCase{},
		&stubUpdateJobUseCase{},
		&stubChangeStatusUseCase{},
	)
	auth := middleware.NewAuth(db)
	router := newRouter(handler, auth, nil)

	// Auth middleware should allow X-OrbitJob-Tenant-Id header
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-OrbitJob-Tenant-Id", "tenant-a")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"status":"ok"`) {
		t.Fatalf("expected healthz body, got %s", resp.Body.String())
	}
}

func TestNewRouter_WithRateLimiter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rl := middleware.NewRateLimiter()

	handler := adminhttp.NewHandler(
		&stubCreateJobUseCase{},
		&stubListJobsUseCase{},
		&stubGetJobUseCase{},
		&stubUpdateJobUseCase{},
		&stubChangeStatusUseCase{},
	)
	router := newRouter(handler, nil, rl)

	// Healthz is in groupPublic so rate limiting is skipped
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}
}

func TestNewRouter_WithAuthAndRateLimiter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	handler := adminhttp.NewHandler(
		&stubCreateJobUseCase{},
		&stubListJobsUseCase{},
		&stubGetJobUseCase{},
		&stubUpdateJobUseCase{},
		&stubChangeStatusUseCase{},
	)
	auth := middleware.NewAuth(db)
	rl := middleware.NewRateLimiter()

	router := newRouter(handler, auth, rl)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-OrbitJob-Tenant-Id", "tenant-a")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"status":"ok"`) {
		t.Fatalf("expected healthz body, got %s", resp.Body.String())
	}
}

func TestNewRouter_MetricsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := newRouter(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}
}

func TestNewRouter_NilHandlerDoesNotRegisterJobRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := newRouter(nil, nil, nil)

	// routes from handler.Register should not exist when handler is nil
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", nil)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	// Gin returns 404 when no route matches (not 405)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status=%d (not found), got %d", http.StatusNotFound, resp.Code)
	}
}

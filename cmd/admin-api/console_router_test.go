package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	runquery "orbitjob/internal/admin/app/run/query"
	"orbitjob/internal/admin/console"
	adminhttp "orbitjob/internal/admin/http"
	"orbitjob/internal/admin/http/middleware"
)

type consoleRouterRuns struct{}

func TestInstallationRootKeepsThePublicOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newRouterWithConsole(adminhttp.NewHandler(nil, nil, nil), nil, nil,
		console.NewHandler(fstest.MapFS{"index.html": {Data: []byte("<html>Console</html>")}}))
	for _, origin := range []string{"http://192.0.2.20:30880", "https://jobs.example.com", "https://jobs.example.com:8443"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			req := httptest.NewRequest(method, origin+"/", nil)
			req.Header.Set("X-Forwarded-Host", "untrusted.example")
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)
			if resp.Code != http.StatusTemporaryRedirect || resp.Header().Get("Location") != "/console/" {
				t.Fatalf("%s %s: status %d, location %q", method, origin, resp.Code, resp.Header().Get("Location"))
			}
			if method == http.MethodHead && resp.Body.Len() != 0 {
				t.Fatalf("HEAD %s returned a response body", origin)
			}
		}
	}
}

func (consoleRouterRuns) List(context.Context, runquery.ListInput) ([]runquery.ListItem, error) {
	return nil, nil
}

func TestPublicConsoleDoesNotExposeAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := adminhttp.NewHandler(nil, nil, nil)
	handler.SetListInstancesUseCase(consoleRouterRuns{})
	router := newRouterWithConsole(handler, middleware.NewAuth(nil), nil,
		console.NewHandler(fstest.MapFS{"index.html": {Data: []byte("<html>Console</html>")}}))
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/console/jobs", http.StatusOK},
		{"/api/v1/instances", http.StatusUnauthorized},
		{"/api/v1/unknown", http.StatusNotFound},
		{"/console/assets/missing.js", http.StatusNotFound},
		{"/healthz", http.StatusOK},
	} {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("Accept", "text/html")
		router.ServeHTTP(resp, req)
		if resp.Code != tc.status {
			t.Fatalf("%s: got %d, want %d", tc.path, resp.Code, tc.status)
		}
		if strings.HasPrefix(tc.path, "/api/") && strings.Contains(resp.Body.String(), "<html>") {
			t.Fatalf("API received the console shell: %s", tc.path)
		}
		if resp.Header().Get("X-Trace-ID") == "" {
			t.Fatalf("trace missing on %s", tc.path)
		}
	}
}

func TestConsoleRemainsPublicWhileAPIDeniesAndLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("RATELIMIT_READ_RPS", "1")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	key := "otj_console_fixture_key"
	hash, err := bcrypt.GenerateFromPassword([]byte(key), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		mock.ExpectQuery("SELECT id, tenant_id, kind, resource_group_id, boundary_policy_id, key_hash, revoked, expired").WithArgs(key[:12]).
			WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "kind", "resource_group_id", "boundary_policy_id", "key_hash", "revoked", "expired"}).
				AddRow("fixture-key", "00000000000000000000000001", "tenant", nil, nil, string(hash), nil, nil))
	}
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	handler := adminhttp.NewHandler(nil, nil, nil)
	handler.SetListInstancesUseCase(consoleRouterRuns{})
	router := newRouterWithConsole(handler, middleware.NewAuth(db), middleware.NewRateLimiter(ctx),
		console.NewHandler(fstest.MapFS{"index.html": {Data: []byte("<html>Console</html>")}}))
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Accept", "text/html")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != status || strings.Contains(resp.Body.String(), "<html>") {
			t.Fatalf("API: %d %s", resp.Code, resp.Body.String())
		}
		public := httptest.NewRecorder()
		navigation := httptest.NewRequest(http.MethodGet, "/console/jobs", nil)
		navigation.Header.Set("Accept", "text/html")
		router.ServeHTTP(public, navigation)
		if public.Code != http.StatusOK || !strings.Contains(public.Body.String(), "<html>") {
			t.Fatalf("console: %d", public.Code)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

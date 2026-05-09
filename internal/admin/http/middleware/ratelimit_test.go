package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRateLimiter_AllowsFirstRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewRateLimiter()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("tenant_id", "test-tenant")
		c.Next()
	})
	r.Use(rl.Middleware())
	r.GET("/api/v1/jobs", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/jobs", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRateLimiter_BlocksWhenExhausted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewRateLimiter()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("tenant_id", "test-tenant")
		c.Next()
	})
	r.Use(rl.Middleware())
	r.POST("/api/v1/jobs", func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	// Write group default is 10 rps, 10 burst. Fire 11 requests.
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/jobs", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("request %d: expected 201, got %d", i, w.Code)
		}
	}

	// 11th should be blocked
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/jobs", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestRateLimiter_SeparateTenants(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewRateLimiter()

	r := gin.New()
	r.Use(rl.Middleware())
	r.POST("/api/v1/jobs", func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	// Exhaust tenant-a (inject via context)
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/jobs", nil)
		req = req.WithContext(WithTenantID(req.Context(), "tenant-a", TenantSourceHeader))
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("tenant-a request %d: expected 201, got %d", i, w.Code)
		}
	}

	// tenant-a should be blocked
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/jobs", nil)
	req = req.WithContext(WithTenantID(req.Context(), "tenant-a", TenantSourceHeader))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("tenant-a 11th request: expected 429, got %d", w.Code)
	}

	// tenant-b should still be allowed (separate bucket)
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/v1/jobs", nil)
	req2 = req2.WithContext(WithTenantID(req2.Context(), "tenant-b", TenantSourceHeader))
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("tenant-b first request: expected 201, got %d", w2.Code)
	}
}

func TestRateLimiter_PublicEndpointsAlwaysAllowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewRateLimiter()

	r := gin.New()
	r.Use(rl.Middleware())
	r.GET("/healthz", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Health endpoint should never be rate limited
	for i := 0; i < 200; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/healthz", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("healthz request %d: expected 200, got %d", i, w.Code)
		}
	}
}

func TestClassifyEndpoint(t *testing.T) {
	tests := []struct {
		method, path string
		want         endpointGroup
	}{
		{"GET", "/healthz", groupPublic},
		{"GET", "/openapi.json", groupPublic},
		{"GET", "/metrics", groupPublic},
		{"POST", "/api/v1/jobs/:id/trigger", groupTrigger},
		{"POST", "/api/v1/instances/:run_id/cancel", groupAdmin},
		{"GET", "/api/v1/jobs", groupRead},
		{"GET", "/api/v1/instances/:run_id", groupRead},
		{"POST", "/api/v1/jobs", groupWrite},
		{"PUT", "/api/v1/jobs/:id", groupWrite},
		{"DELETE", "/api/v1/jobs/:id", groupWrite},
	}

	for _, tt := range tests {
		got := classifyEndpoint(tt.method, tt.path)
		if got != tt.want {
			t.Errorf("classifyEndpoint(%q, %q) = %q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}

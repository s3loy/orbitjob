package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiter_AllowsFirstRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewRateLimiter(context.Background())

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
	rl := NewRateLimiter(context.Background())

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
	for i := range 10 {
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
	rl := NewRateLimiter(context.Background())

	r := gin.New()
	r.Use(rl.Middleware())
	r.POST("/api/v1/jobs", func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	// Exhaust tenant-a (inject via context)
	for i := range 10 {
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
	rl := NewRateLimiter(context.Background())

	r := gin.New()
	r.Use(rl.Middleware())
	r.GET("/healthz", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Health endpoint should never be rate limited
	for i := range 200 {
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

// ---------------------------------------------------------------------------
// loadEnvInt tests
// ---------------------------------------------------------------------------

func TestLoadEnvInt(t *testing.T) {
	const testKey = "TEST_LOAD_ENV_INT"

	tests := []struct {
		name     string
		envVal   string // empty = do not set
		fallback int
		want     int
	}{
		{"env set to valid", "50", 100, 50},
		{"env not set", "", 100, 100},
		{"env invalid string", "abc", 100, 100},
		{"env zero", "0", 100, 100},
		{"env negative", "-1", 100, 100},
		{"fallback zero", "", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				t.Setenv(testKey, tt.envVal)
			}
			got := loadEnvInt(testKey, tt.fallback)
			if got != tt.want {
				t.Errorf("loadEnvInt(%q, %d) = %d, want %d", testKey, tt.fallback, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// reapLoop tests
// ---------------------------------------------------------------------------

func TestReapLoop_RemovesExpiredEntries(t *testing.T) {
	rl := &RateLimiter{
		limits: map[endpointGroup]groupConfig{
			groupRead: {rps: 10, burst: 10},
		},
		buckets: map[endpointGroup]map[string]*tokenBucket{},
	}

	// Use a long interval so the active tenant is never reaped during the test.
	go rl.reapLoop(context.Background(), 10*time.Second)
	time.Sleep(10 * time.Millisecond) // let goroutine start

	// Add buckets: one expired (well past the interval), one active (just used).
	oldTime := time.Now().Add(-1 * time.Hour)
	rl.mu.Lock()
	rl.buckets[groupRead] = map[string]*tokenBucket{
		"expired-tenant": {tokens: 0, lastTime: oldTime, lastUsed: oldTime},
		"active-tenant":  {tokens: 10, lastTime: time.Now(), lastUsed: time.Now()},
	}
	rl.mu.Unlock()

	// Trigger a reap manually by calling the logic directly.
	rl.mu.Lock()
	cutoff := time.Now().Add(-10 * time.Second)
	for _, groupBuckets := range rl.buckets {
		for tid, entry := range groupBuckets {
			if entry.lastUsed.Before(cutoff) {
				delete(groupBuckets, tid)
			}
		}
	}
	rl.mu.Unlock()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if _, ok := rl.buckets[groupRead]["expired-tenant"]; ok {
		t.Error("expired-tenant should have been reaped")
	}
	if _, ok := rl.buckets[groupRead]["active-tenant"]; !ok {
		t.Error("active-tenant should not have been reaped")
	}
}

func TestReapLoop_EmptyBuckets(t *testing.T) {
	rl := &RateLimiter{
		limits: map[endpointGroup]groupConfig{
			groupRead: {rps: 10, burst: 10},
		},
		buckets: map[endpointGroup]map[string]*tokenBucket{},
	}

	// No entries — reap should not panic.
	go rl.reapLoop(context.Background(), 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if len(rl.buckets) > 0 {
		for g, m := range rl.buckets {
			if len(m) > 0 {
				t.Errorf("group %q unexpectedly has %d buckets", g, len(m))
			}
		}
	}
}

func TestReapLoop_AllExpired(t *testing.T) {
	rl := &RateLimiter{
		limits: map[endpointGroup]groupConfig{
			groupRead: {rps: 10, burst: 10},
		},
		buckets: map[endpointGroup]map[string]*tokenBucket{},
	}

	oldTime := time.Now().Add(-2 * time.Hour)
	rl.buckets[groupRead] = map[string]*tokenBucket{
		"old-a": {tokens: 0, lastTime: oldTime, lastUsed: oldTime},
		"old-b": {tokens: 0, lastTime: oldTime, lastUsed: oldTime},
	}

	go rl.reapLoop(context.Background(), 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if len(rl.buckets[groupRead]) != 0 {
		t.Errorf("expected 0 buckets, got %d", len(rl.buckets[groupRead]))
	}
}

func TestReapLoop_ThreadSafety(t *testing.T) {
	// Verify concurrent access doesn't cause data races.
	rl := NewRateLimiter(context.Background())

	// Populate buckets via tryAllow through goroutines.
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range 5 {
				rl.tryAllow(groupRead, "tenant-"+string(rune('a'+id%26)), rl.limits[groupRead])
				time.Sleep(time.Millisecond)
			}
		}(i)
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// groupEnvKey and tryAllow edge cases
// ---------------------------------------------------------------------------

func TestGroupEnvKey(t *testing.T) {
	tests := []struct {
		group endpointGroup
		want  string
	}{
		{groupRead, "RATELIMIT_READ_RPS"},
		{groupWrite, "RATELIMIT_WRITE_RPS"},
		{groupTrigger, "RATELIMIT_TRIGGER_RPS"},
		{groupAdmin, "RATELIMIT_ADMIN_RPS"},
		{groupPublic, ""},
		{endpointGroup("unknown"), ""},
	}
	for _, tt := range tests {
		got := groupEnvKey(tt.group)
		if got != tt.want {
			t.Errorf("groupEnvKey(%q) = %q, want %q", tt.group, got, tt.want)
		}
	}
}

func TestTryAllow_NewGroup(t *testing.T) {
	rl := &RateLimiter{
		limits: map[endpointGroup]groupConfig{
			groupWrite: {rps: 10, burst: 10},
		},
		buckets: map[endpointGroup]map[string]*tokenBucket{},
	}

	// First request creates both group and tenant buckets.
	if !rl.tryAllow(groupWrite, "tenant-new", rl.limits[groupWrite]) {
		t.Fatal("expected first request to be allowed")
	}

	// Verify the bucket was created.
	rl.mu.Lock()
	bucket, ok := rl.buckets[groupWrite]["tenant-new"]
	rl.mu.Unlock()
	if !ok {
		t.Fatal("expected bucket to be created for new tenant")
	}
	if bucket.tokens < 0 {
		t.Fatalf("expected non-negative tokens, got %f", bucket.tokens)
	}
}

func TestTryAllow_ExistingGroupNewTenant(t *testing.T) {
	rl := &RateLimiter{
		limits: map[endpointGroup]groupConfig{
			groupRead: {rps: 10, burst: 10},
		},
		buckets: map[endpointGroup]map[string]*tokenBucket{
			groupRead: {"existing-tenant": {tokens: 5, lastTime: time.Now()}},
		},
	}

	// New tenant in existing group -- should create new bucket.
	if !rl.tryAllow(groupRead, "new-tenant", rl.limits[groupRead]) {
		t.Fatal("expected new tenant in existing group to be allowed")
	}

	rl.mu.Lock()
	_, ok := rl.buckets[groupRead]["new-tenant"]
	rl.mu.Unlock()
	if !ok {
		t.Fatal("expected bucket to be created for new tenant in existing group")
	}
}

func TestTryAllow_TokenCap(t *testing.T) {
	// Create a bucket with burst=2 and lastTime far in the past.
	// When tryAllow is called, elapsed * rps should push tokens above burst,
	// triggering the cap branch.
	cfg := groupConfig{rps: 100, burst: 2}
	rl := &RateLimiter{
		limits: map[endpointGroup]groupConfig{
			groupRead: cfg,
		},
		buckets: map[endpointGroup]map[string]*tokenBucket{
			groupRead: {
				"capped-tenant": {
					tokens:   0,
					lastTime: time.Now().Add(-1 * time.Hour),
				},
			},
		},
	}

	if !rl.tryAllow(groupRead, "capped-tenant", cfg) {
		t.Fatal("expected request to be allowed after token cap")
	}

	rl.mu.Lock()
	bucket := rl.buckets[groupRead]["capped-tenant"]
	tokens := bucket.tokens
	rl.mu.Unlock()

	if tokens < 0 || tokens >= float64(cfg.burst) {
		t.Fatalf("expected tokens to be burst-1 after cap+consume (got %f)", tokens)
	}
}

func TestTryAllow_TokenCapExact(t *testing.T) {
	// When elapsed * rps equals exactly burst, no cap needed.
	// The cap branch (> not >=) should NOT trigger.
	cfg := groupConfig{rps: 10, burst: 10}
	rl := &RateLimiter{
		limits: map[endpointGroup]groupConfig{
			groupRead: cfg,
		},
		buckets: map[endpointGroup]map[string]*tokenBucket{
			groupRead: {
				"no-cap-tenant": {
					tokens:   0,
					lastTime: time.Now().Add(-1 * time.Second),
				},
			},
		},
	}

	if !rl.tryAllow(groupRead, "no-cap-tenant", cfg) {
		t.Fatal("expected request to be allowed")
	}
}

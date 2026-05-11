package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/admin/http/middleware"
)

// --- loadDevPositiveInt ---

func TestLoadDevPositiveInt_Default(t *testing.T) {
	t.Setenv("NONEXISTENT_VAR", "") // ensure clean state
	v, err := loadDevPositiveInt("NONEXISTENT_VAR_XYZ", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 42 {
		t.Fatalf("expected default 42, got %d", v)
	}
}

func TestLoadDevPositiveInt_EnvSet(t *testing.T) {
	t.Setenv("TEST_INT_VAL", "7")
	v, err := loadDevPositiveInt("TEST_INT_VAL", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 7 {
		t.Fatalf("expected 7, got %d", v)
	}
}

func TestLoadDevPositiveInt_Invalid(t *testing.T) {
	t.Setenv("TEST_INT_BAD", "abc")
	_, err := loadDevPositiveInt("TEST_INT_BAD", 42)
	if err == nil {
		t.Fatal("expected error for non-int value")
	}
}

func TestLoadDevPositiveInt_Zero(t *testing.T) {
	t.Setenv("TEST_INT_ZERO", "0")
	_, err := loadDevPositiveInt("TEST_INT_ZERO", 42)
	if err == nil {
		t.Fatal("expected error for zero")
	}
}

func TestLoadDevPositiveInt_Negative(t *testing.T) {
	t.Setenv("TEST_INT_NEG", "-5")
	_, err := loadDevPositiveInt("TEST_INT_NEG", 42)
	if err == nil {
		t.Fatal("expected error for negative")
	}
}

// --- loadDevSchedulerConfig ---

func TestLoadDevSchedulerConfig_Defaults(t *testing.T) {
	cfg := loadDevSchedulerConfig()
	if cfg.BatchSize != 100 {
		t.Fatalf("expected BatchSize=100, got %d", cfg.BatchSize)
	}
	if cfg.TickInterval != 5*1_000_000_000 { // 5s in ns
		t.Fatalf("expected TickInterval=5s, got %v", cfg.TickInterval)
	}
}

func TestLoadDevSchedulerConfig_CustomEnv(t *testing.T) {
	t.Setenv("SCHEDULER_BATCH_SIZE", "200")
	t.Setenv("SCHEDULER_TICK_INTERVAL_SEC", "10")
	cfg := loadDevSchedulerConfig()
	if cfg.BatchSize != 200 {
		t.Fatalf("expected BatchSize=200, got %d", cfg.BatchSize)
	}
	if cfg.TickInterval != 10*1_000_000_000 {
		t.Fatalf("expected TickInterval=10s, got %v", cfg.TickInterval)
	}
}

func TestLoadDevSchedulerConfig_InvalidFallback(t *testing.T) {
	t.Setenv("SCHEDULER_BATCH_SIZE", "not-a-number")
	cfg := loadDevSchedulerConfig()
	// Invalid values fall back to 0 (loadDevPositiveInt returns 0 on error)
	if cfg.BatchSize != 0 {
		t.Fatalf("expected BatchSize=0 (fallback on parse error), got %d", cfg.BatchSize)
	}
}

// --- loadDevDispatcherConfig ---

func TestLoadDevDispatcherConfig_Defaults(t *testing.T) {
	cfg := loadDevDispatcherConfig()
	if cfg.TenantID != "default" {
		t.Fatalf("expected TenantID=default, got %q", cfg.TenantID)
	}
	if cfg.BatchSize != 50 {
		t.Fatalf("expected BatchSize=50, got %d", cfg.BatchSize)
	}
	if cfg.TickInterval != 2*1_000_000_000 {
		t.Fatalf("expected TickInterval=2s, got %v", cfg.TickInterval)
	}
	if cfg.LeaseDuration != 30*1_000_000_000 {
		t.Fatalf("expected LeaseDuration=30s, got %v", cfg.LeaseDuration)
	}
}

func TestLoadDevDispatcherConfig_CustomTenant(t *testing.T) {
	t.Setenv("DISPATCHER_TENANT_ID", "tenant-custom")
	cfg := loadDevDispatcherConfig()
	if cfg.TenantID != "tenant-custom" {
		t.Fatalf("expected TenantID=tenant-custom, got %q", cfg.TenantID)
	}
}

func TestLoadDevDispatcherConfig_CustomValues(t *testing.T) {
	t.Setenv("DISPATCHER_BATCH_SIZE", "100")
	t.Setenv("DISPATCHER_TICK_INTERVAL_SEC", "5")
	t.Setenv("DISPATCHER_LEASE_DURATION_SEC", "60")
	cfg := loadDevDispatcherConfig()
	if cfg.BatchSize != 100 {
		t.Fatalf("expected BatchSize=100, got %d", cfg.BatchSize)
	}
	if cfg.LeaseDuration != 60*1_000_000_000 {
		t.Fatalf("expected LeaseDuration=60s, got %v", cfg.LeaseDuration)
	}
}

// --- loadDevWorkerConfig ---

func TestLoadDevWorkerConfig_Defaults(t *testing.T) {
	cfg := loadDevWorkerConfig()
	if cfg.TenantID != "default" {
		t.Fatalf("expected TenantID=default, got %q", cfg.TenantID)
	}
	if cfg.WorkerID == "" {
		t.Fatal("expected non-empty WorkerID")
	}
	if cfg.PollInterval != 2*1_000_000_000 {
		t.Fatalf("expected PollInterval=2s, got %v", cfg.PollInterval)
	}
	if cfg.HeartbeatInterval != 10*1_000_000_000 {
		t.Fatalf("expected HeartbeatInterval=10s, got %v", cfg.HeartbeatInterval)
	}
	if cfg.LeaseDuration != 60*1_000_000_000 {
		t.Fatalf("expected LeaseDuration=60s, got %v", cfg.LeaseDuration)
	}
	if cfg.Capacity != 1 {
		t.Fatalf("expected Capacity=1, got %d", cfg.Capacity)
	}
}

func TestLoadDevWorkerConfig_CustomWorkerID(t *testing.T) {
	t.Setenv("WORKER_ID", "custom-worker")
	cfg := loadDevWorkerConfig()
	if cfg.WorkerID != "custom-worker" {
		t.Fatalf("expected WorkerID=custom-worker, got %q", cfg.WorkerID)
	}
}

func TestLoadDevWorkerConfig_CustomValues(t *testing.T) {
	t.Setenv("WORKER_TENANT_ID", "tenant-w")
	t.Setenv("WORKER_POLL_INTERVAL_SEC", "3")
	t.Setenv("WORKER_HEARTBEAT_INTERVAL_SEC", "15")
	t.Setenv("WORKER_LEASE_DURATION_SEC", "90")
	t.Setenv("WORKER_CAPACITY", "5")
	cfg := loadDevWorkerConfig()
	if cfg.TenantID != "tenant-w" {
		t.Fatalf("expected TenantID=tenant-w, got %q", cfg.TenantID)
	}
	if cfg.Capacity != 5 {
		t.Fatalf("expected Capacity=5, got %d", cfg.Capacity)
	}
}

// --- traceMiddleware ---

func TestTraceMiddleware_GeneratesTraceID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.TraceMiddleware())
	r.GET("/test", func(c *gin.Context) {
		traceID, _ := c.Get("trace_id")
		if traceID == nil || traceID == "" {
			t.Fatal("expected trace_id to be set")
		}
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("X-Trace-ID") == "" {
		t.Fatal("expected X-Trace-ID header to be set")
	}
}

func TestTraceMiddleware_ForwardsExistingTraceID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.TraceMiddleware())
	r.GET("/test", func(c *gin.Context) {
		traceID, _ := c.Get("trace_id")
		if traceID != "existing-trace-123" {
			t.Fatalf("expected trace_id=existing-trace-123, got %v", traceID)
		}
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Trace-ID", "existing-trace-123")
	r.ServeHTTP(w, req)

	if w.Header().Get("X-Trace-ID") != "existing-trace-123" {
		t.Fatalf("expected X-Trace-ID header to forward existing value")
	}
}

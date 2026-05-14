package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	domainworker "orbitjob/internal/core/domain/worker"
)

// ---------------------------------------------------------------------------
// loadPositiveIntEnv edge cases
// ---------------------------------------------------------------------------

func TestLoadPositiveIntEnv_Zero(t *testing.T) {
	t.Setenv("TEST_WORKER_KEY", "0")

	_, err := loadPositiveIntEnv("TEST_WORKER_KEY", 100)
	if err == nil || !strings.Contains(err.Error(), "must be >= 1") {
		t.Fatalf("expected 'must be >= 1' error, got %v", err)
	}
}

func TestLoadPositiveIntEnv_Negative(t *testing.T) {
	t.Setenv("TEST_WORKER_KEY", "-1")

	_, err := loadPositiveIntEnv("TEST_WORKER_KEY", 100)
	if err == nil || !strings.Contains(err.Error(), "must be >= 1") {
		t.Fatalf("expected 'must be >= 1' error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// sendHeartbeat
// ---------------------------------------------------------------------------

type errorHeartbeater struct {
	err error
}

func (h *errorHeartbeater) UpsertHeartbeat(_ context.Context, _ domainworker.HeartbeatSpec) (domainworker.Snapshot, error) {
	return domainworker.Snapshot{}, h.err
}

func (h *errorHeartbeater) GetByID(_ context.Context, _, _ string) (domainworker.Snapshot, error) {
	return domainworker.Snapshot{}, nil
}

func TestSendHeartbeat_UpsertError(t *testing.T) {
	hb := &errorHeartbeater{err: errors.New("db write failed")}
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	cfg := runtimeConfig{
		TenantID:      "t1",
		WorkerID:      "w1",
		LeaseDuration: 60 * time.Second,
		Capacity:      1,
		Labels:        map[string]any{},
	}

	// This should not panic — error is logged, not returned
	sendHeartbeat(context.Background(), hb, cfg, func() time.Time { return now }, domainworker.StatusOnline)
}

func TestSendHeartbeat_NormalizeError(t *testing.T) {
	hb := &errorHeartbeater{}
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	cfg := runtimeConfig{
		TenantID:      "t1",
		WorkerID:      "", // empty workerID triggers normalize error
		LeaseDuration: 60 * time.Second,
		Capacity:      1,
		Labels:        map[string]any{},
	}

	// This should not panic — normalize error is logged, not returned
	sendHeartbeat(context.Background(), hb, cfg, func() time.Time { return now }, domainworker.StatusOnline)
}

// ---------------------------------------------------------------------------
// loadWorkerRuntimeConfig edge cases
// ---------------------------------------------------------------------------

func TestLoadWorkerRuntimeConfig_InvalidPollInterval(t *testing.T) {
	t.Setenv("WORKER_ID", "worker-1")
	t.Setenv("WORKER_TENANT_ID", "")
	t.Setenv("WORKER_POLL_INTERVAL_SEC", "0")
	t.Setenv("WORKER_HEARTBEAT_INTERVAL_SEC", "")
	t.Setenv("WORKER_LEASE_DURATION_SEC", "")
	t.Setenv("WORKER_CAPACITY", "")
	t.Setenv("WORKER_LABELS", "")

	_, err := loadWorkerRuntimeConfig()
	if err == nil || !strings.Contains(err.Error(), "must be >= 1") {
		t.Fatalf("expected 'must be >= 1' error for poll interval, got %v", err)
	}
}

func TestLoadWorkerRuntimeConfig_InvalidLeaseDuration(t *testing.T) {
	t.Setenv("WORKER_ID", "worker-1")
	t.Setenv("WORKER_TENANT_ID", "")
	t.Setenv("WORKER_POLL_INTERVAL_SEC", "")
	t.Setenv("WORKER_HEARTBEAT_INTERVAL_SEC", "")
	t.Setenv("WORKER_LEASE_DURATION_SEC", "-1")
	t.Setenv("WORKER_CAPACITY", "")
	t.Setenv("WORKER_LABELS", "")

	_, err := loadWorkerRuntimeConfig()
	if err == nil || !strings.Contains(err.Error(), "must be >= 1") {
		t.Fatalf("expected 'must be >= 1' error for lease duration, got %v", err)
	}
}

func TestLoadWorkerRuntimeConfig_InvalidCapacity(t *testing.T) {
	t.Setenv("WORKER_ID", "worker-1")
	t.Setenv("WORKER_TENANT_ID", "")
	t.Setenv("WORKER_POLL_INTERVAL_SEC", "")
	t.Setenv("WORKER_HEARTBEAT_INTERVAL_SEC", "")
	t.Setenv("WORKER_LEASE_DURATION_SEC", "")
	t.Setenv("WORKER_CAPACITY", "0")
	t.Setenv("WORKER_LABELS", "")

	_, err := loadWorkerRuntimeConfig()
	if err == nil || !strings.Contains(err.Error(), "must be >= 1") {
		t.Fatalf("expected 'must be >= 1' error for capacity, got %v", err)
	}
}

func TestLoadWorkerRuntimeConfig_InvalidHeartbeatInterval(t *testing.T) {
	t.Setenv("WORKER_ID", "worker-1")
	t.Setenv("WORKER_TENANT_ID", "")
	t.Setenv("WORKER_POLL_INTERVAL_SEC", "")
	t.Setenv("WORKER_HEARTBEAT_INTERVAL_SEC", "bad")
	t.Setenv("WORKER_LEASE_DURATION_SEC", "")
	t.Setenv("WORKER_CAPACITY", "")
	t.Setenv("WORKER_LABELS", "")

	_, err := loadWorkerRuntimeConfig()
	if err == nil || !strings.Contains(err.Error(), "must be an integer") {
		t.Fatalf("expected 'must be an integer' error for heartbeat interval, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// heartbeatLoop — tests for the ctx.Done() shutdown path
// ---------------------------------------------------------------------------

func TestHeartbeatLoop_ShutdownGraceful(t *testing.T) {
	// Test that heartbeatLoop sends draining -> offline on shutdown
	// when loopDone closes before shutdownDeadline
	ctx, cancel := context.WithCancel(context.Background())
	loopDone := make(chan struct{})
	hb := &stubHeartbeater{}

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	cfg := runtimeConfig{
		TenantID:          "t1",
		WorkerID:          "w1",
		HeartbeatInterval: time.Second,
		LeaseDuration:     60 * time.Second,
		Capacity:          1,
		Labels:            map[string]any{},
	}

	ticker := newFakeTicker()

	done := make(chan struct{})
	go func() {
		heartbeatLoop(ctx, loopDone, hb, cfg,
			func(time.Duration) workerTicker { return ticker },
			func() time.Time { return now })
		close(done)
	}()

	// Wait for first heartbeat (online)
	time.Sleep(50 * time.Millisecond)
	// Cancel context to trigger shutdown
	cancel()

	// Signal that loopDone will close immediately (simulating no in-flight tasks)
	close(loopDone)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("heartbeatLoop did not return after shutdown")
	}

	// Should have sent: online (initial), draining (shutdown phase 1), offline (shutdown phase 3)
	if hb.callCount() < 3 {
		t.Fatalf("expected at least 3 heartbeats (online+draining+offline), got %d", hb.callCount())
	}

	// Last heartbeat should be offline
	if last := hb.lastStatus(); last != domainworker.StatusOffline {
		t.Fatalf("expected last status=offline, got %q", last)
	}
}

// ---------------------------------------------------------------------------
// runLoop error path (SubmitNext returns error)
// ---------------------------------------------------------------------------

func TestRunLoop_SubmitNextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := newFakeTicker()
	runner := &stubTickRunner{
		err: errors.New("execution failed"),
		onCall: func(callNo int) {
			if callNo == 1 {
				cancel()
			}
		},
	}
	hb := &stubHeartbeater{}

	done := make(chan struct{})
	go func() {
		runLoop(ctx, runner, hb, runtimeConfig{
			TenantID:          "t1",
			WorkerID:          "w1",
			PollInterval:      time.Second,
			HeartbeatInterval: time.Second,
			LeaseDuration:     60 * time.Second,
			Capacity:          1,
			Labels:            map[string]any{},
		}, func(time.Duration) workerTicker {
			return ticker
		}, func() time.Time { return time.Now().UTC() })
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runLoop did not stop on error + cancel")
	}

	// Error path still goes through outer select -> ctx.Done() -> return
	if runner.callCount() != 1 {
		t.Fatalf("expected 1 SubmitNext call (no drain when handled=0), got %d", runner.callCount())
	}
}

// ---------------------------------------------------------------------------
// run() config error path
// ---------------------------------------------------------------------------

func TestRun_ConfigError(t *testing.T) {
	resetWorkerMainDeps(t)
	t.Setenv("DATABASE_DSN", "postgres://unit-test")
	t.Setenv("WORKER_CAPACITY", "bad")

	loadDotenvFn = func() error { return nil }
	newLoggerFn = func(string) *slog.Logger { return slog.Default() }

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "WORKER_CAPACITY must be an integer") {
		t.Fatalf("expected WORKER_CAPACITY error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// loadJSONMapEnv
// ---------------------------------------------------------------------------

func TestLoadJSONMapEnv_Empty(t *testing.T) {
	t.Setenv("TEST_WORKER_LABELS_KEY", "")

	labels, err := loadJSONMapEnv("TEST_WORKER_LABELS_KEY")
	if err != nil {
		t.Fatalf("expected no error for empty labels, got %v", err)
	}
	if len(labels) != 0 {
		t.Fatalf("expected empty map, got %v", labels)
	}
}

func TestLoadJSONMapEnv_Valid(t *testing.T) {
	t.Setenv("TEST_WORKER_LABELS_KEY", `{"gpu":"a100","pool":"high"}`)

	labels, err := loadJSONMapEnv("TEST_WORKER_LABELS_KEY")
	if err != nil {
		t.Fatalf("expected no error for valid JSON, got %v", err)
	}
	if labels["gpu"] != "a100" {
		t.Fatalf("expected gpu=a100, got %v", labels["gpu"])
	}
	if labels["pool"] != "high" {
		t.Fatalf("expected pool=high, got %v", labels["pool"])
	}
}

func TestLoadJSONMapEnv_Invalid(t *testing.T) {
	t.Setenv("TEST_WORKER_LABELS_KEY", "not-json")

	_, err := loadJSONMapEnv("TEST_WORKER_LABELS_KEY")
	if err == nil || !strings.Contains(err.Error(), "must be valid JSON") {
		t.Fatalf("expected 'must be valid JSON' error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// loadFloatEnv edge cases
// ---------------------------------------------------------------------------

func TestLoadFloatEnv_Empty(t *testing.T) {
	// Ensure env is unset.
	t.Setenv("TEST_WORKER_FLOAT", "")

	v, err := loadFloatEnv("TEST_WORKER_FLOAT", 0.5)
	if err != nil {
		t.Fatalf("expected no error for empty env, got %v", err)
	}
	if v != 0.5 {
		t.Fatalf("expected default value 0.5, got %v", v)
	}
}

func TestLoadFloatEnv_Valid(t *testing.T) {
	t.Setenv("TEST_WORKER_FLOAT", "0.75")

	v, err := loadFloatEnv("TEST_WORKER_FLOAT", 0.5)
	if err != nil {
		t.Fatalf("expected no error for valid float, got %v", err)
	}
	if v != 0.75 {
		t.Fatalf("expected value 0.75, got %v", v)
	}
}

func TestLoadFloatEnv_Invalid(t *testing.T) {
	t.Setenv("TEST_WORKER_FLOAT", "not-a-float")

	_, err := loadFloatEnv("TEST_WORKER_FLOAT", 0.5)
	if err == nil || !strings.Contains(err.Error(), "must be a float") {
		t.Fatalf("expected 'must be a float' error, got %v", err)
	}
}

func TestLoadFloatEnv_Zero(t *testing.T) {
	t.Setenv("TEST_WORKER_FLOAT", "0")

	_, err := loadFloatEnv("TEST_WORKER_FLOAT", 0.5)
	if err == nil || !strings.Contains(err.Error(), "must be in (0, 1]") {
		t.Fatalf("expected 'must be in (0, 1]' error, got %v", err)
	}
}

func TestLoadFloatEnv_AboveOne(t *testing.T) {
	t.Setenv("TEST_WORKER_FLOAT", "1.5")

	_, err := loadFloatEnv("TEST_WORKER_FLOAT", 0.5)
	if err == nil || !strings.Contains(err.Error(), "must be in (0, 1]") {
		t.Fatalf("expected 'must be in (0, 1]' error, got %v", err)
	}
}

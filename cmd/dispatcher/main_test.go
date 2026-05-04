package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	domaininstance "orbitjob/internal/core/domain/instance"
)

type stubTickRunner struct {
	mu     sync.Mutex
	calls  int
	err    error
	limits []int
	specs  []domaininstance.ClaimSpec
	callCh chan struct{}
	onCall func(int)

	handled int
}

func (s *stubTickRunner) RunBatch(ctx context.Context, spec domaininstance.ClaimSpec, limit int) (int, error) {
	s.mu.Lock()
	s.calls++
	callNo := s.calls
	s.limits = append(s.limits, limit)
	s.specs = append(s.specs, spec)
	onCall := s.onCall
	err := s.err
	handled := s.handled
	callCh := s.callCh
	s.mu.Unlock()

	if callCh != nil {
		select {
		case callCh <- struct{}{}:
		default:
		}
	}
	if onCall != nil {
		onCall(callNo)
	}

	return handled, err
}

func (s *stubTickRunner) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubTickRunner) lastLimit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.limits) == 0 {
		return 0
	}
	return s.limits[len(s.limits)-1]
}

func (s *stubTickRunner) lastSpec() domaininstance.ClaimSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.specs) == 0 {
		return domaininstance.ClaimSpec{}
	}
	return s.specs[len(s.specs)-1]
}

type fakeTicker struct {
	ch      chan time.Time
	stopped chan struct{}
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{
		ch:      make(chan time.Time, 1),
		stopped: make(chan struct{}, 1),
	}
}

func (f *fakeTicker) Chan() <-chan time.Time {
	return f.ch
}

func (f *fakeTicker) Stop() {
	select {
	case f.stopped <- struct{}{}:
	default:
	}
}

func resetDispatcherMainDeps(t *testing.T) {
	t.Helper()

	oldLoadDotenvFn := loadDotenvFn
	oldNewLoggerFn := newLoggerFn
	oldOpenDBFn := openDBFn
	oldPingDBFn := pingDBFn
	oldBuildRunnerFn := buildRunnerFn
	oldRunLoopFn := runLoopFn

	t.Cleanup(func() {
		loadDotenvFn = oldLoadDotenvFn
		newLoggerFn = oldNewLoggerFn
		openDBFn = oldOpenDBFn
		pingDBFn = oldPingDBFn
		buildRunnerFn = oldBuildRunnerFn
		runLoopFn = oldRunLoopFn
	})
}

func TestLoadDispatcherRuntimeConfig_Custom(t *testing.T) {
	t.Setenv("DISPATCHER_TENANT_ID", "tenant-42")
	t.Setenv("DISPATCHER_BATCH_SIZE", "100")
	t.Setenv("DISPATCHER_TICK_INTERVAL_SEC", "5")
	t.Setenv("DISPATCHER_LEASE_DURATION_SEC", "60")

	cfg, err := loadDispatcherRuntimeConfig()
	if err != nil {
		t.Fatalf("loadDispatcherRuntimeConfig() error = %v", err)
	}
	if cfg.TenantID != "tenant-42" {
		t.Fatalf("expected tenantID=tenant-42, got %q", cfg.TenantID)
	}
	if cfg.BatchSize != 100 {
		t.Fatalf("expected batch size=100, got %d", cfg.BatchSize)
	}
	if cfg.TickInterval != 5*time.Second {
		t.Fatalf("expected tick interval=5s, got %s", cfg.TickInterval)
	}
	if cfg.LeaseDuration != 60*time.Second {
		t.Fatalf("expected lease duration=60s, got %s", cfg.LeaseDuration)
	}
}

func TestLoadDispatcherRuntimeConfig_Defaults(t *testing.T) {
	t.Setenv("DISPATCHER_TENANT_ID", "")
	t.Setenv("DISPATCHER_BATCH_SIZE", "")
	t.Setenv("DISPATCHER_TICK_INTERVAL_SEC", "")
	t.Setenv("DISPATCHER_LEASE_DURATION_SEC", "")

	cfg, err := loadDispatcherRuntimeConfig()
	if err != nil {
		t.Fatalf("loadDispatcherRuntimeConfig() error = %v", err)
	}
	if cfg.TenantID != "default" {
		t.Fatalf("expected default tenantID, got %q", cfg.TenantID)
	}
	if cfg.BatchSize != 50 {
		t.Fatalf("expected default batch size=50, got %d", cfg.BatchSize)
	}
	if cfg.TickInterval != 2*time.Second {
		t.Fatalf("expected default tick interval=2s, got %s", cfg.TickInterval)
	}
	if cfg.LeaseDuration != 30*time.Second {
		t.Fatalf("expected default lease duration=30s, got %s", cfg.LeaseDuration)
	}
}

func TestLoadDispatcherRuntimeConfig_InvalidBatchSize(t *testing.T) {
	t.Setenv("DISPATCHER_TENANT_ID", "")
	t.Setenv("DISPATCHER_BATCH_SIZE", "abc")
	t.Setenv("DISPATCHER_TICK_INTERVAL_SEC", "")
	t.Setenv("DISPATCHER_LEASE_DURATION_SEC", "")

	if _, err := loadDispatcherRuntimeConfig(); err == nil {
		t.Fatalf("expected error for invalid batch size")
	}
}

func TestRunLoop_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	ticker := newFakeTicker()
	runner := &stubTickRunner{
		handled: 1,
		onCall: func(callNo int) {
			if callNo == 1 {
				cancel()
			}
		},
	}

	done := make(chan struct{})
	go func() {
		runLoop(ctx, runner, runtimeConfig{
			TenantID:      "t1",
			BatchSize:     7,
			TickInterval:  time.Second,
			LeaseDuration: 30 * time.Second,
		}, func(time.Duration) schedulerTicker {
			return ticker
		}, func() time.Time { return now })
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("runLoop did not stop after context cancellation")
	}

	if runner.callCount() != 1 {
		t.Fatalf("expected one RunBatch call, got %d", runner.callCount())
	}
	if runner.lastLimit() != 7 {
		t.Fatalf("expected limit=7, got %d", runner.lastLimit())
	}
	spec := runner.lastSpec()
	if spec.TenantID != "t1" {
		t.Fatalf("expected spec.TenantID=t1, got %q", spec.TenantID)
	}
	if !spec.Now.Equal(now) {
		t.Fatalf("expected spec.Now=%v, got %v", now, spec.Now)
	}
	expectedLease := now.Add(30 * time.Second)
	if !spec.LeaseExpiresAt.Equal(expectedLease) {
		t.Fatalf("expected LeaseExpiresAt=%v, got %v", expectedLease, spec.LeaseExpiresAt)
	}

	select {
	case <-ticker.stopped:
	case <-time.After(time.Second):
		t.Fatalf("expected ticker.Stop() to be called")
	}
}

func TestRunLoop_ContinuesAfterTickSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := newFakeTicker()
	runner := &stubTickRunner{callCh: make(chan struct{}, 4)}
	runner.onCall = func(callNo int) {
		if callNo == 2 {
			cancel()
		}
	}

	done := make(chan struct{})
	go func() {
		runLoop(ctx, runner, runtimeConfig{
			TenantID:      "t1",
			BatchSize:     3,
			TickInterval:  time.Second,
			LeaseDuration: 30 * time.Second,
		}, func(time.Duration) schedulerTicker {
			return ticker
		}, func() time.Time { return time.Now().UTC() })
		close(done)
	}()

	select {
	case <-runner.callCh:
	case <-time.After(time.Second):
		t.Fatalf("expected first RunBatch call")
	}

	ticker.ch <- time.Now()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("runLoop did not stop after second iteration")
	}

	if runner.callCount() != 2 {
		t.Fatalf("expected two RunBatch calls, got %d", runner.callCount())
	}
}

func TestRunLoop_ErrorPathStillWaitsForShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := newFakeTicker()
	runner := &stubTickRunner{
		err: errors.New("boom"),
		onCall: func(callNo int) {
			if callNo == 1 {
				cancel()
			}
		},
	}

	done := make(chan struct{})
	go func() {
		runLoop(ctx, runner, runtimeConfig{
			TenantID:      "t1",
			BatchSize:     1,
			TickInterval:  time.Second,
			LeaseDuration: 30 * time.Second,
		}, func(time.Duration) schedulerTicker {
			return ticker
		}, func() time.Time { return time.Now() })
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("runLoop did not stop on error-path cancellation")
	}

	if runner.callCount() != 1 {
		t.Fatalf("expected one RunBatch call on error path, got %d", runner.callCount())
	}
}

func TestNewWallClockTicker_ChanAndStop(t *testing.T) {
	ticker := newWallClockTicker(10 * time.Millisecond)
	if ticker.Chan() == nil {
		t.Fatalf("expected ticker channel to be non-nil")
	}
	ticker.Stop()
}

func TestRun_LoadDotenvError(t *testing.T) {
	resetDispatcherMainDeps(t)

	loadDotenvFn = func() error { return errors.New("dotenv boom") }

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "dotenv boom") {
		t.Fatalf("expected dotenv error, got %v", err)
	}
}

func TestRun_DatabaseDSNRequired(t *testing.T) {
	resetDispatcherMainDeps(t)
	t.Setenv("DATABASE_DSN", "")

	loadDotenvFn = func() error { return nil }
	newLoggerFn = func(string) *slog.Logger { return slog.Default() }

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "DATABASE_DSN is required") {
		t.Fatalf("expected DATABASE_DSN required error, got %v", err)
	}
}

func TestRun_OpenDBError(t *testing.T) {
	resetDispatcherMainDeps(t)
	t.Setenv("DATABASE_DSN", "postgres://unit-test")
	t.Setenv("DISPATCHER_TENANT_ID", "")
	t.Setenv("DISPATCHER_BATCH_SIZE", "")
	t.Setenv("DISPATCHER_TICK_INTERVAL_SEC", "")
	t.Setenv("DISPATCHER_LEASE_DURATION_SEC", "")

	loadDotenvFn = func() error { return nil }
	newLoggerFn = func(string) *slog.Logger { return slog.Default() }
	openDBFn = func(string) (*sql.DB, error) { return nil, errors.New("open boom") }

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "open boom") {
		t.Fatalf("expected open db error, got %v", err)
	}
}

func TestRun_SuccessInvokesRunLoop(t *testing.T) {
	resetDispatcherMainDeps(t)
	t.Setenv("DATABASE_DSN", "postgres://unit-test")
	t.Setenv("DISPATCHER_TENANT_ID", "tenant-42")
	t.Setenv("DISPATCHER_BATCH_SIZE", "9")
	t.Setenv("DISPATCHER_TICK_INTERVAL_SEC", "3")
	t.Setenv("DISPATCHER_LEASE_DURATION_SEC", "45")
	t.Setenv("APP_ENV", "test")

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	loadDotenvFn = func() error { return nil }
	newLoggerFn = func(env string) *slog.Logger {
		if env != "test" {
			t.Fatalf("expected APP_ENV=test, got %q", env)
		}
		return slog.Default()
	}
	openDBFn = func(dsn string) (*sql.DB, error) {
		if dsn != "postgres://unit-test" {
			t.Fatalf("unexpected dsn: %q", dsn)
		}
		return db, nil
	}
	pingDBFn = func(_ context.Context, gotDB *sql.DB) error {
		if gotDB != db {
			t.Fatalf("expected ping to use opened db")
		}
		return nil
	}

	stub := &stubTickRunner{}
	buildRunnerFn = func(gotDB *sql.DB) tickRunner {
		if gotDB != db {
			t.Fatalf("expected buildRunner to receive opened db")
		}
		return stub
	}

	runLoopCalled := false
	runLoopFn = func(
		ctx context.Context,
		runner tickRunner,
		cfg runtimeConfig,
		newTicker func(time.Duration) schedulerTicker,
		nowFn func() time.Time,
	) {
		runLoopCalled = true
		if runner != stub {
			t.Fatalf("expected injected runner")
		}
		if cfg.TenantID != "tenant-42" {
			t.Fatalf("expected tenantID=tenant-42, got %q", cfg.TenantID)
		}
		if cfg.BatchSize != 9 {
			t.Fatalf("expected batch size=9, got %d", cfg.BatchSize)
		}
		if cfg.TickInterval != 3*time.Second {
			t.Fatalf("expected tick interval=3s, got %s", cfg.TickInterval)
		}
		if cfg.LeaseDuration != 45*time.Second {
			t.Fatalf("expected lease duration=45s, got %s", cfg.LeaseDuration)
		}
		ticker := newTicker(time.Millisecond)
		if ticker.Chan() == nil {
			t.Fatalf("expected non-nil ticker channel")
		}
		ticker.Stop()
		if nowFn().IsZero() {
			t.Fatalf("expected nowFn to return a non-zero time")
		}
	}

	if err := run(context.Background()); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !runLoopCalled {
		t.Fatalf("expected runLoop to be invoked")
	}
}

func TestRun_PingDBError(t *testing.T) {
	resetDispatcherMainDeps(t)
	t.Setenv("DATABASE_DSN", "postgres://unit-test")
	t.Setenv("DISPATCHER_TENANT_ID", "")
	t.Setenv("DISPATCHER_BATCH_SIZE", "")
	t.Setenv("DISPATCHER_TICK_INTERVAL_SEC", "")
	t.Setenv("DISPATCHER_LEASE_DURATION_SEC", "")

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	loadDotenvFn = func() error { return nil }
	newLoggerFn = func(string) *slog.Logger { return slog.Default() }
	openDBFn = func(string) (*sql.DB, error) { return db, nil }
	pingDBFn = func(context.Context, *sql.DB) error { return errors.New("ping boom") }

	runLoopCalled := false
	runLoopFn = func(context.Context, tickRunner, runtimeConfig, func(time.Duration) schedulerTicker, func() time.Time) {
		runLoopCalled = true
	}

	err = run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ping database: ping boom") {
		t.Fatalf("expected ping database error, got %v", err)
	}
	if runLoopCalled {
		t.Fatalf("expected runLoop not to run when ping fails")
	}
}

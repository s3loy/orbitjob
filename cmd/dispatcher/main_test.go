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
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/election"
)

type stubTickRunner struct {
	mu     sync.Mutex
	calls  int
	err    error
	limits []int
	specs  []domaininstance.ClaimSpec
	callCh chan struct{}
	onCall func(int)

	handled         int
	housekeepingErr error

	tenantIDs []string
	tenantErr error
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

func (s *stubTickRunner) QuickTick(ctx context.Context, spec domaininstance.ClaimSpec, limit int) (int, error) {
	return s.RunBatch(ctx, spec, limit)
}

func (s *stubTickRunner) ListActiveTenantIDs(_ context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tenantErr != nil {
		return nil, s.tenantErr
	}
	if s.tenantIDs == nil {
		return []string{"default"}, nil
	}
	return s.tenantIDs, nil
}

func (s *stubTickRunner) RunHousekeeping(_ context.Context, _ time.Time) error {
	return s.housekeepingErr
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
	ch        chan time.Time
	stopped   chan struct{}
	stopCount int
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
	f.stopCount++
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
	oldNewEventListenerFn := newEventListenerFn

	t.Cleanup(func() {
		loadDotenvFn = oldLoadDotenvFn
		newLoggerFn = oldNewLoggerFn
		openDBFn = oldOpenDBFn
		pingDBFn = oldPingDBFn
		buildRunnerFn = oldBuildRunnerFn
		runLoopFn = oldRunLoopFn
		newEventListenerFn = oldNewEventListenerFn
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
	if cfg.TenantID != "" {
		t.Fatalf("expected empty tenantID when DISPATCHER_TENANT_ID not set, got %q", cfg.TenantID)
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
		runLoop(ctx, runner, &runtimeConfig{
			TenantID:      "t1",
			BatchSize:     7,
			TickInterval:  time.Second,
			LeaseDuration: 30 * time.Second,
		}, nil, func(time.Duration) schedulerTicker {
			return ticker
		}, func() time.Time { return now }, nil)
		close(done)
	}()

	// Trigger first tick to start the loop
	ticker.ch <- now

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("runLoop did not stop after context cancellation")
	}

	if runner.callCount() != 2 {
		t.Fatalf("expected two RunBatch calls (1 tick + 1 drain), got %d", runner.callCount())
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
		runLoop(ctx, runner, &runtimeConfig{
			TenantID:      "t1",
			BatchSize:     3,
			TickInterval:  time.Second,
			LeaseDuration: 30 * time.Second,
		}, nil, func(time.Duration) schedulerTicker {
			return ticker
		}, func() time.Time { return time.Now().UTC() }, nil)
		close(done)
	}()

	// Trigger first tick
	ticker.ch <- time.Now()

	select {
	case <-runner.callCh:
	case <-time.After(time.Second):
		t.Fatalf("expected first RunBatch call")
	}

	// Trigger second tick; onCall will cancel context on callNo==2
	ticker.ch <- time.Now()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("runLoop did not stop after second iteration")
	}

	if runner.callCount() != 3 {
		t.Fatalf("expected three RunBatch calls (2 ticks + 1 drain), got %d", runner.callCount())
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
		runLoop(ctx, runner, &runtimeConfig{
			TenantID:      "t1",
			BatchSize:     1,
			TickInterval:  time.Second,
			LeaseDuration: 30 * time.Second,
		}, nil, func(time.Duration) schedulerTicker {
			return ticker
		}, func() time.Time { return time.Now() }, nil)
		close(done)
	}()

	// Trigger first tick
	ticker.ch <- time.Now()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("runLoop did not stop on error-path cancellation")
	}

	if runner.callCount() != 2 {
		t.Fatalf("expected two RunBatch calls on error path (1 tick + 1 drain), got %d", runner.callCount())
	}
}

func TestRunLoop_EventChQuickTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := newFakeTicker()
	eventCh := make(chan struct{}, 1)
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
		runLoop(ctx, runner, &runtimeConfig{
			TenantID:      "t1",
			BatchSize:     5,
			TickInterval:  time.Second,
			LeaseDuration: 30 * time.Second,
		}, eventCh, func(time.Duration) schedulerTicker {
			return ticker
		}, func() time.Time { return time.Now().UTC() }, nil)
		close(done)
	}()

	// Trigger event-driven quick tick
	eventCh <- struct{}{}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("runLoop did not stop after event quick tick")
	}

	if runner.callCount() != 2 {
		t.Fatalf("expected two QuickTick calls (1 event + 1 drain), got %d", runner.callCount())
	}
	if runner.lastLimit() != 5 {
		t.Fatalf("expected limit=5, got %d", runner.lastLimit())
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
	t.Setenv("RUNTIME_DSN", "")
	t.Setenv("DISPATCHER_DSN", "")
	t.Setenv("DATABASE_DSN", "")

	loadDotenvFn = func() error { return nil }
	newLoggerFn = func(string) *slog.Logger { return slog.Default() }

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "RUNTIME_DSN or DISPATCHER_DSN or DATABASE_DSN is required") {
		t.Fatalf("expected database DSN required error, got %v", err)
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
	t.Setenv("RUNTIME_DSN", "postgres://runtime-unit-test")
	t.Setenv("DATABASE_DSN", "postgres://legacy-unit-test")
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
		if dsn != "postgres://runtime-unit-test" {
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

	newEventListenerFn = func(string) (*corepostgres.EventListener, error) {
		return nil, errors.New("no listener in test")
	}

	runLoopCalled := false
	runLoopFn = func(
		ctx context.Context,
		runner tickRunner,
		cfg *runtimeConfig,
		_ <-chan struct{},
		newTicker func(time.Duration) schedulerTicker,
		nowFn func() time.Time,
		_ election.Coordinator,
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
	runLoopFn = func(context.Context, tickRunner, *runtimeConfig, <-chan struct{}, func(time.Duration) schedulerTicker, func() time.Time, election.Coordinator) {
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

func TestQuickTick_ErrorPath(t *testing.T) {
	runner := &stubTickRunner{err: errors.New("tick boom")}
	cfg := runtimeConfig{BatchSize: 5, LeaseDuration: 30 * time.Second}
	now := time.Now()
	result := quickTick(context.Background(), runner, &cfg, now, []string{"t1", "t2"}, nil)
	if result != 0 {
		t.Fatalf("expected 0 handled on error, got %d", result)
	}
	if runner.callCount() != 2 {
		t.Fatalf("expected 2 calls for 2 tenants, got %d", runner.callCount())
	}
}

func TestFullTick_HousekeepingError(t *testing.T) {
	runner := &stubTickRunner{handled: 1, housekeepingErr: errors.New("hk boom")}
	cfg := runtimeConfig{BatchSize: 5, LeaseDuration: 30 * time.Second}
	now := time.Now()
	result := fullTick(context.Background(), runner, &cfg, now, []string{"t1"}, nil)
	if result != 1 {
		t.Fatalf("expected 1 handled despite housekeeping error, got %d", result)
	}
}

func TestFullTick_TickError(t *testing.T) {
	runner := &stubTickRunner{err: errors.New("tick boom")}
	cfg := runtimeConfig{BatchSize: 5, LeaseDuration: 30 * time.Second}
	now := time.Now()
	result := fullTick(context.Background(), runner, &cfg, now, []string{"t1", "t2"}, nil)
	if result != 0 {
		t.Fatalf("expected 0 handled on tick error, got %d", result)
	}
}

func TestRunLoop_SwitchesToLongInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := newFakeTicker()
	runner := &stubTickRunner{}
	runner.onCall = func(callNo int) {
		if callNo == 3 {
			// After 3 idle ticks, next tick should use long interval
			cancel()
		}
	}

	done := make(chan struct{})
	go func() {
		runLoop(ctx, runner, &runtimeConfig{
			TenantID:      "t1",
			BatchSize:     3,
			TickInterval:  time.Second,
			LeaseDuration: 30 * time.Second,
		}, nil, func(time.Duration) schedulerTicker {
			return ticker
		}, func() time.Time { return time.Now().UTC() }, nil)
		close(done)
	}()

	// Trigger 3 idle ticks
	for i := 0; i < 3; i++ {
		ticker.ch <- time.Now()
		select {
		case <-ticker.stopped:
			// ticker.Stop() called on long-interval switch
		case <-time.After(200 * time.Millisecond):
		}
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("runLoop did not stop")
	}

	if ticker.stopCount < 1 {
		t.Fatalf("expected ticker.Stop() at least once for long interval switch, got %d", ticker.stopCount)
	}
}

func TestRunLoop_SwitchesBackToShortInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := newFakeTicker()
	runner := &stubTickRunner{}
	runner.onCall = func(n int) {
		if n == 4 {
			// 3 idle ticks (switched to long) + 1 handled tick (switch back) + drain
			cancel()
		}
	}

	done := make(chan struct{})
	go func() {
		runLoop(ctx, runner, &runtimeConfig{
			TenantID:      "t1",
			BatchSize:     3,
			TickInterval:  time.Second,
			LeaseDuration: 30 * time.Second,
		}, nil, func(time.Duration) schedulerTicker {
			return ticker
		}, func() time.Time { return time.Now().UTC() }, nil)
		close(done)
	}()

	// 3 idle ticks to trigger long interval
	for i := 0; i < 3; i++ {
		ticker.ch <- time.Now()
		select {
		case <-ticker.stopped:
		case <-time.After(200 * time.Millisecond):
		}
	}

	// 4th tick with work to switch back
	runner.mu.Lock()
	runner.handled = 1
	runner.mu.Unlock()
	ticker.ch <- time.Now()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("runLoop did not stop")
	}

	// Should have stopped ticker twice: once for long interval, once for switching back
	if ticker.stopCount < 2 {
		t.Fatalf("expected ticker.Stop() at least twice (long + switch back), got %d", ticker.stopCount)
	}
}

func TestRunLoop_MultiTenantDiscoversAndDispatches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := newFakeTicker()
	runner := &stubTickRunner{
		handled:   1,
		tenantIDs: []string{"tenant-a", "tenant-b"},
		onCall: func(callNo int) {
			if callNo == 2 {
				cancel()
			}
		},
	}

	done := make(chan struct{})
	go func() {
		runLoop(ctx, runner, &runtimeConfig{
			BatchSize:     5,
			TickInterval:  time.Second,
			LeaseDuration: 30 * time.Second,
		}, nil, func(time.Duration) schedulerTicker {
			return ticker
		}, func() time.Time { return time.Now().UTC() }, nil)
		close(done)
	}()

	ticker.ch <- time.Now()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("runLoop did not stop")
	}

	if runner.callCount() != 4 {
		t.Fatalf("expected 4 RunBatch calls (2 tenants x 2 ticks), got %d", runner.callCount())
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.specs[0].TenantID != "tenant-a" || runner.specs[1].TenantID != "tenant-b" {
		t.Fatalf("expected first tick tenants tenant-a and tenant-b, got %+v", runner.specs)
	}
}

func TestRunLoop_MultiTenantListErrorNoFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := newFakeTicker()
	runner := &stubTickRunner{
		tenantErr: errors.New("db down"),
		onCall: func(callNo int) {
			if callNo == 0 {
				// RunBatch should not be called when tenant discovery fails.
				t.Fatalf("RunBatch should not be called when tenant discovery fails")
			}
		},
	}

	done := make(chan struct{})
	go func() {
		runLoop(ctx, runner, &runtimeConfig{
			BatchSize:     5,
			TickInterval:  time.Second,
			LeaseDuration: 30 * time.Second,
		}, nil, func(time.Duration) schedulerTicker {
			return ticker
		}, func() time.Time { return time.Now().UTC() }, nil)
		close(done)
	}()

	ticker.ch <- time.Now()

	// Give it a moment to process the idle tick, then cancel.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("runLoop did not stop")
	}

	if runner.callCount() != 0 {
		t.Fatalf("expected 0 RunBatch calls when tenant discovery fails, got %d", runner.callCount())
	}
}

func TestRunLoop_MultiTenantEmptyListNoFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := newFakeTicker()
	runner := &stubTickRunner{
		tenantIDs: []string{},
		onCall: func(callNo int) {
			if callNo == 0 {
				t.Fatalf("RunBatch should not be called when tenant list is empty")
			}
		},
	}

	done := make(chan struct{})
	go func() {
		runLoop(ctx, runner, &runtimeConfig{
			BatchSize:     5,
			TickInterval:  time.Second,
			LeaseDuration: 30 * time.Second,
		}, nil, func(time.Duration) schedulerTicker {
			return ticker
		}, func() time.Time { return time.Now().UTC() }, nil)
		close(done)
	}()

	ticker.ch <- time.Now()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("runLoop did not stop")
	}

	if runner.callCount() != 0 {
		t.Fatalf("expected 0 RunBatch calls when tenant list is empty, got %d", runner.callCount())
	}
}

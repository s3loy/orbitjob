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

	"orbitjob/internal/core/app/schedule"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

type stubTickRunner struct {
	mu      sync.Mutex
	calls   int
	err     error
	limits  []int
	times   []time.Time
	callCh  chan struct{}
	onCall  func(int)
	handled int
}

func noopProbe(ctx context.Context) (time.Duration, error) {
	return 1 * time.Millisecond, nil
}

func noopQueueDepth(ctx context.Context) (int64, error) {
	return 0, nil
}

func (s *stubTickRunner) RunBatch(ctx context.Context, now time.Time, limit int) (schedule.BatchCounts, error) {
	s.mu.Lock()
	s.calls++
	callNo := s.calls
	s.limits = append(s.limits, limit)
	s.times = append(s.times, now)
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

	return schedule.BatchCounts{Handled: handled, Scheduled: handled}, err
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

func resetSchedulerMainDeps(t *testing.T) {
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

func TestLoadSchedulerRuntimeConfig_Defaults(t *testing.T) {
	t.Setenv("SCHEDULER_BATCH_SIZE_MAX", "")
	t.Setenv("SCHEDULER_TICK_INTERVAL_SEC", "")

	cfg, err := loadSchedulerRuntimeConfig()
	if err != nil {
		t.Fatalf("loadSchedulerRuntimeConfig() error = %v", err)
	}
	if cfg.BatchSizeMax != 500 {
		t.Fatalf("expected default max batch size=500, got %d", cfg.BatchSizeMax)
	}
	if cfg.TickInterval != 5*time.Second {
		t.Fatalf("expected default tick interval=5s, got %s", cfg.TickInterval)
	}
}

func TestLoadSchedulerRuntimeConfig_Custom(t *testing.T) {
	t.Setenv("SCHEDULER_BATCH_SIZE_MAX", "250")
	t.Setenv("SCHEDULER_TICK_INTERVAL_SEC", "2")

	cfg, err := loadSchedulerRuntimeConfig()
	if err != nil {
		t.Fatalf("loadSchedulerRuntimeConfig() error = %v", err)
	}
	if cfg.BatchSizeMax != 250 {
		t.Fatalf("expected max batch size=250, got %d", cfg.BatchSizeMax)
	}
	if cfg.TickInterval != 2*time.Second {
		t.Fatalf("expected tick interval=2s, got %s", cfg.TickInterval)
	}
}

func TestLoadSchedulerRuntimeConfig_InvalidBatchSize(t *testing.T) {
	t.Setenv("SCHEDULER_BATCH_SIZE_MAX", "abc")
	t.Setenv("SCHEDULER_TICK_INTERVAL_SEC", "")

	if _, err := loadSchedulerRuntimeConfig(); err == nil {
		t.Fatalf("expected error for invalid batch size")
	}
}

func TestLoadSchedulerRuntimeConfig_InvalidTickInterval(t *testing.T) {
	t.Setenv("SCHEDULER_BATCH_SIZE_MAX", "")
	t.Setenv("SCHEDULER_TICK_INTERVAL_SEC", "0")

	if _, err := loadSchedulerRuntimeConfig(); err == nil {
		t.Fatalf("expected error for invalid tick interval")
	}
}

func TestRunLoop_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
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
		runLoop(ctx, runner, noopProbe, noopQueueDepth, &runtimeConfig{BatchSizeMax: 7, TickInterval: time.Second}, func(time.Duration) schedulerTicker {
			return ticker
		}, func() time.Time { return now })
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("runLoop did not stop after context cancellation")
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
		runLoop(ctx, runner, noopProbe, noopQueueDepth, &runtimeConfig{BatchSizeMax: 3, TickInterval: time.Second}, func(time.Duration) schedulerTicker {
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
		runLoop(ctx, runner, noopProbe, noopQueueDepth, &runtimeConfig{BatchSizeMax: 1, TickInterval: time.Second}, func(time.Duration) schedulerTicker {
			return ticker
		}, func() time.Time { return time.Now() })
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("runLoop did not stop on error-path cancellation")
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
	resetSchedulerMainDeps(t)

	loadDotenvFn = func() error { return errors.New("dotenv boom") }

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "dotenv boom") {
		t.Fatalf("expected dotenv error, got %v", err)
	}
}

func TestRun_DatabaseDSNRequired(t *testing.T) {
	resetSchedulerMainDeps(t)
	t.Setenv("DATABASE_DSN", "")

	loadDotenvFn = func() error { return nil }
	newLoggerFn = func(string) *slog.Logger { return slog.Default() }

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "DATABASE_DSN is required") {
		t.Fatalf("expected DATABASE_DSN required error, got %v", err)
	}
}

func TestRun_OpenDBError(t *testing.T) {
	resetSchedulerMainDeps(t)
	t.Setenv("DATABASE_DSN", "postgres://unit-test")
	t.Setenv("SCHEDULER_BATCH_SIZE_MAX", "")
	t.Setenv("SCHEDULER_TICK_INTERVAL_SEC", "")

	loadDotenvFn = func() error { return nil }
	newLoggerFn = func(string) *slog.Logger { return slog.Default() }
	openDBFn = func(string) (*sql.DB, error) { return nil, errors.New("open boom") }

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "open boom") {
		t.Fatalf("expected open db error, got %v", err)
	}
}

func TestRun_SuccessInvokesRunLoop(t *testing.T) {
	resetSchedulerMainDeps(t)
	t.Setenv("DATABASE_DSN", "postgres://unit-test")
	t.Setenv("SCHEDULER_BATCH_SIZE_MAX", "9")
	t.Setenv("SCHEDULER_TICK_INTERVAL_SEC", "3")
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
		probe func(context.Context) (time.Duration, error),
		queueDepth func(context.Context) (int64, error),
		cfg *runtimeConfig,
		newTicker func(time.Duration) schedulerTicker,
		nowFn func() time.Time,
	) {
		runLoopCalled = true
		if runner != stub {
			t.Fatalf("expected injected runner")
		}
		if cfg.BatchSizeMax != 9 {
			t.Fatalf("expected max batch size=9, got %d", cfg.BatchSizeMax)
		}
		if cfg.TickInterval != 3*time.Second {
			t.Fatalf("expected tick interval=3s, got %s", cfg.TickInterval)
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
	resetSchedulerMainDeps(t)
	t.Setenv("DATABASE_DSN", "postgres://unit-test")
	t.Setenv("SCHEDULER_BATCH_SIZE_MAX", "")
	t.Setenv("SCHEDULER_TICK_INTERVAL_SEC", "")

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
	runLoopFn = func(context.Context, tickRunner, func(context.Context) (time.Duration, error), func(context.Context) (int64, error), *runtimeConfig, func(time.Duration) schedulerTicker, func() time.Time) {
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

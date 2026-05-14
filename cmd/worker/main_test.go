package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/app/execute"
	domainworker "orbitjob/internal/core/domain/worker"
)

// ---------------------------------------------------------------------------
// stubs
// ---------------------------------------------------------------------------

type stubTickRunner struct {
	mu      sync.Mutex
	calls   int
	err     error
	handled int
	onCall  func(int)
	callCh  chan struct{}
}

func (s *stubTickRunner) SubmitNext(_ context.Context, _ *execute.WorkerPool, _, _ string, _ int, _ time.Duration, _ map[string]any) (int, error) {
	s.mu.Lock()
	s.calls++
	callNo := s.calls
	err := s.err
	handled := s.handled
	callCh := s.callCh
	onCall := s.onCall
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

type stubHeartbeater struct {
	mu    sync.Mutex
	calls []domainworker.HeartbeatSpec
}

func (s *stubHeartbeater) UpsertHeartbeat(_ context.Context, spec domainworker.HeartbeatSpec) (domainworker.Snapshot, error) {
	s.mu.Lock()
	s.calls = append(s.calls, spec)
	s.mu.Unlock()
	return domainworker.Snapshot{}, nil
}

func (s *stubHeartbeater) GetByID(_ context.Context, _, _ string) (domainworker.Snapshot, error) {
	return domainworker.Snapshot{Status: domainworker.StatusOnline}, nil
}

func (s *stubHeartbeater) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

var _ = (&stubHeartbeater{}).callCount

func (s *stubHeartbeater) lastStatus() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		return ""
	}
	return s.calls[len(s.calls)-1].Status
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

func (f *fakeTicker) Chan() <-chan time.Time { return f.ch }
func (f *fakeTicker) Stop() {
	select {
	case f.stopped <- struct{}{}:
	default:
	}
}

func resetWorkerMainDeps(t *testing.T) {
	t.Helper()

	oldLoadDotenvFn := loadDotenvFn
	oldNewLoggerFn := newLoggerFn
	oldOpenDBFn := openDBFn
	oldPingDBFn := pingDBFn
	oldBuildRunnerFn := buildRunnerFn
	oldBuildHeartbeaterFn := buildHeartbeaterFn
	oldRunLoopFn := runLoopFn

	t.Cleanup(func() {
		loadDotenvFn = oldLoadDotenvFn
		newLoggerFn = oldNewLoggerFn
		openDBFn = oldOpenDBFn
		pingDBFn = oldPingDBFn
		buildRunnerFn = oldBuildRunnerFn
		buildHeartbeaterFn = oldBuildHeartbeaterFn
		runLoopFn = oldRunLoopFn
	})
}

// ---------------------------------------------------------------------------
// config tests
// ---------------------------------------------------------------------------

func TestLoadWorkerRuntimeConfig_Defaults(t *testing.T) {
	t.Setenv("WORKER_ID", "")

	cfg, err := loadWorkerRuntimeConfig()
	if err != nil {
		t.Fatalf("loadWorkerRuntimeConfig() error = %v", err)
	}
	if cfg.WorkerID == "" {
		t.Fatalf("expected auto-generated worker ID")
	}
}

func TestLoadWorkerRuntimeConfig_Custom(t *testing.T) {
	t.Setenv("WORKER_ID", "worker-1")
	t.Setenv("WORKER_TENANT_ID", "tenant-42")
	t.Setenv("WORKER_POLL_INTERVAL_SEC", "5")
	t.Setenv("WORKER_HEARTBEAT_INTERVAL_SEC", "15")
	t.Setenv("WORKER_LEASE_DURATION_SEC", "120")
	t.Setenv("WORKER_CAPACITY", "4")
	t.Setenv("WORKER_LABELS", `{"gpu":"a100"}`)

	cfg, err := loadWorkerRuntimeConfig()
	if err != nil {
		t.Fatalf("loadWorkerRuntimeConfig() error = %v", err)
	}
	if cfg.WorkerID != "worker-1" {
		t.Fatalf("expected workerID=worker-1, got %q", cfg.WorkerID)
	}
	if cfg.TenantID != "tenant-42" {
		t.Fatalf("expected tenantID=tenant-42, got %q", cfg.TenantID)
	}
	if cfg.PollInterval != 5*time.Second {
		t.Fatalf("expected poll interval=5s, got %s", cfg.PollInterval)
	}
	if cfg.HeartbeatInterval != 15*time.Second {
		t.Fatalf("expected heartbeat interval=15s, got %s", cfg.HeartbeatInterval)
	}
	if cfg.LeaseDuration != 120*time.Second {
		t.Fatalf("expected lease duration=120s, got %s", cfg.LeaseDuration)
	}
	if cfg.Capacity != 4 {
		t.Fatalf("expected capacity=4, got %d", cfg.Capacity)
	}
	if cfg.Labels["gpu"] != "a100" {
		t.Fatalf("expected labels[gpu]=a100, got %v", cfg.Labels)
	}
}

func TestLoadWorkerRuntimeConfig_InvalidLabels(t *testing.T) {
	t.Setenv("WORKER_ID", "worker-1")
	t.Setenv("WORKER_TENANT_ID", "")
	t.Setenv("WORKER_POLL_INTERVAL_SEC", "")
	t.Setenv("WORKER_HEARTBEAT_INTERVAL_SEC", "")
	t.Setenv("WORKER_LEASE_DURATION_SEC", "")
	t.Setenv("WORKER_CAPACITY", "")
	t.Setenv("WORKER_LABELS", "not-json")

	_, err := loadWorkerRuntimeConfig()
	if err == nil || !strings.Contains(err.Error(), "WORKER_LABELS must be valid JSON") {
		t.Fatalf("expected JSON error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// runLoop tests
// ---------------------------------------------------------------------------

func TestRunLoop_DrainMode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := newFakeTicker()
	runner := &stubTickRunner{
		handled: 1,
		onCall: func(callNo int) {
			if callNo == 2 {
				cancel()
			}
		},
	}
	hb := &stubHeartbeater{}

	done := make(chan struct{})
	go func() {
		runLoop(ctx, runner, hb, &runtimeConfig{
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
		t.Fatal("runLoop did not stop")
	}

	if runner.callCount() < 2 {
		t.Fatalf("expected at least 2 calls (drain mode), got %d", runner.callCount())
	}
}

func TestRunLoop_WaitsTickerWhenIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := newFakeTicker()
	runner := &stubTickRunner{
		handled: 0,
		callCh:  make(chan struct{}, 4),
		onCall: func(callNo int) {
			if callNo == 2 {
				cancel()
			}
		},
	}
	hb := &stubHeartbeater{}

	done := make(chan struct{})
	go func() {
		runLoop(ctx, runner, hb, &runtimeConfig{
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

	<-runner.callCh
	ticker.ch <- time.Now()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runLoop did not stop after ticker fire + cancel")
	}

	if runner.callCount() != 2 {
		t.Fatalf("expected 2 calls, got %d", runner.callCount())
	}
}

func TestRunLoop_HeartbeatSendsOfflineOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	ticker := newFakeTicker()
	runner := &stubTickRunner{
		onCall: func(int) { cancel() },
	}
	hb := &stubHeartbeater{}

	done := make(chan struct{})
	go func() {
		runLoop(ctx, runner, hb, &runtimeConfig{
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
	case <-time.After(5 * time.Second):
		t.Fatal("runLoop did not stop")
	}

	time.Sleep(50 * time.Millisecond)
	if hb.lastStatus() != domainworker.StatusOffline {
		t.Fatalf("expected last heartbeat status=offline, got %q", hb.lastStatus())
	}
}

// ---------------------------------------------------------------------------
// run() tests
// ---------------------------------------------------------------------------

func TestRun_LoadDotenvError(t *testing.T) {
	resetWorkerMainDeps(t)

	loadDotenvFn = func() error { return errors.New("dotenv boom") }

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "dotenv boom") {
		t.Fatalf("expected dotenv error, got %v", err)
	}
}

func TestRun_DatabaseDSNRequired(t *testing.T) {
	resetWorkerMainDeps(t)
	t.Setenv("DATABASE_DSN", "")

	loadDotenvFn = func() error { return nil }
	newLoggerFn = func(string) *slog.Logger { return slog.Default() }

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "DATABASE_DSN is required") {
		t.Fatalf("expected DATABASE_DSN required error, got %v", err)
	}
}

func TestRun_OpenDBError(t *testing.T) {
	resetWorkerMainDeps(t)
	t.Setenv("DATABASE_DSN", "postgres://unit-test")
	t.Setenv("WORKER_ID", "worker-1")
	t.Setenv("WORKER_TENANT_ID", "")
	t.Setenv("WORKER_POLL_INTERVAL_SEC", "")
	t.Setenv("WORKER_HEARTBEAT_INTERVAL_SEC", "")
	t.Setenv("WORKER_LEASE_DURATION_SEC", "")
	t.Setenv("WORKER_CAPACITY", "")
	t.Setenv("WORKER_LABELS", "")

	loadDotenvFn = func() error { return nil }
	newLoggerFn = func(string) *slog.Logger { return slog.Default() }
	openDBFn = func(string) (*sql.DB, error) { return nil, errors.New("open boom") }

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "open boom") {
		t.Fatalf("expected open db error, got %v", err)
	}
}

func TestRun_PingDBError(t *testing.T) {
	resetWorkerMainDeps(t)
	t.Setenv("DATABASE_DSN", "postgres://unit-test")
	t.Setenv("WORKER_ID", "worker-1")
	t.Setenv("WORKER_TENANT_ID", "")
	t.Setenv("WORKER_POLL_INTERVAL_SEC", "")
	t.Setenv("WORKER_HEARTBEAT_INTERVAL_SEC", "")
	t.Setenv("WORKER_LEASE_DURATION_SEC", "")
	t.Setenv("WORKER_CAPACITY", "")
	t.Setenv("WORKER_LABELS", "")

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	loadDotenvFn = func() error { return nil }
	newLoggerFn = func(string) *slog.Logger { return slog.Default() }
	openDBFn = func(string) (*sql.DB, error) { return db, nil }
	pingDBFn = func(context.Context, *sql.DB) error { return errors.New("ping boom") }

	err = run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ping database: ping boom") {
		t.Fatalf("expected ping database error, got %v", err)
	}
}

func TestRun_SuccessInvokesRunLoop(t *testing.T) {
	resetWorkerMainDeps(t)
	t.Setenv("DATABASE_DSN", "postgres://unit-test")
	t.Setenv("WORKER_ID", "worker-1")
	t.Setenv("WORKER_TENANT_ID", "tenant-42")
	t.Setenv("WORKER_POLL_INTERVAL_SEC", "3")
	t.Setenv("WORKER_HEARTBEAT_INTERVAL_SEC", "15")
	t.Setenv("WORKER_LEASE_DURATION_SEC", "120")
	t.Setenv("WORKER_CAPACITY", "4")
	t.Setenv("WORKER_LABELS", `{"gpu":"a100"}`)
	t.Setenv("APP_ENV", "test")

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	loadDotenvFn = func() error { return nil }
	newLoggerFn = func(string) *slog.Logger { return slog.Default() }
	openDBFn = func(string) (*sql.DB, error) { return db, nil }
	pingDBFn = func(context.Context, *sql.DB) error { return nil }
	buildRunnerFn = func(*sql.DB, *http.Client) tickRunner { return &stubTickRunner{} }
	buildHeartbeaterFn = func(*sql.DB) heartbeater { return &stubHeartbeater{} }

	runLoopCalled := false
	runLoopFn = func(
		_ context.Context,
		_ tickRunner,
		_ heartbeater,
		cfg *runtimeConfig,
		_ func(time.Duration) workerTicker,
		_ func() time.Time,
	) {
		runLoopCalled = true
		if cfg.TenantID != "tenant-42" {
			t.Fatalf("expected tenantID=tenant-42, got %q", cfg.TenantID)
		}
		if cfg.WorkerID != "worker-1" {
			t.Fatalf("expected workerID=worker-1, got %q", cfg.WorkerID)
		}
		if cfg.PollInterval != 3*time.Second {
			t.Fatalf("expected poll interval=3s, got %s", cfg.PollInterval)
		}
		if cfg.LeaseDuration != 120*time.Second {
			t.Fatalf("expected lease duration=120s, got %s", cfg.LeaseDuration)
		}
		if cfg.Capacity != 4 {
			t.Fatalf("expected capacity=4, got %d", cfg.Capacity)
		}
	}

	if err := run(context.Background()); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !runLoopCalled {
		t.Fatal("expected runLoop to be invoked")
	}
}

func TestNewWallClockTicker_ChanAndStop(t *testing.T) {
	ticker := newWallClockTicker(10 * time.Millisecond)
	if ticker.Chan() == nil {
		t.Fatal("expected ticker channel to be non-nil")
	}
	ticker.Stop()
}

// ---------------------------------------------------------------------------
// adaptiveTickRunner tests
// ---------------------------------------------------------------------------

func TestAdaptiveTickRunner_ProbeAndQuery(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	inner := &stubTickRunner{handled: 3}
	ac := execute.NewAdaptiveCapacity(10, 3)
	dl := execute.NewDynamicLease(10*time.Second, 5*time.Minute, 0.1)

	runner := &adaptiveTickRunner{
		inner:        inner,
		db:           db,
		adaptiveCap:  ac,
		dynamicLease: dl,
	}

	mock.ExpectPing()
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM job_instances").WithArgs("t1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(10))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM workers").WithArgs("t1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	n, err := runner.SubmitNext(context.Background(), nil, "t1", "w1", 1, 60*time.Second, map[string]any{})
	if err != nil {
		t.Fatalf("SubmitNext() error = %v", err)
	}
	if n != 3 {
		t.Fatalf("expected n=3, got %d", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

type capturingTickRunner struct {
	stubTickRunner
	lastLimit        int
	lastLease        time.Duration
}

func (c *capturingTickRunner) SubmitNext(ctx context.Context, pool *execute.WorkerPool, tenantID, workerID string, limit int, leaseDuration time.Duration, labels map[string]any) (int, error) {
	c.lastLimit = limit
	c.lastLease = leaseDuration
	return c.stubTickRunner.SubmitNext(ctx, pool, tenantID, workerID, limit, leaseDuration, labels)
}

func TestAdaptiveTickRunner_AdaptiveCapNil(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	inner := &capturingTickRunner{}

	runner := &adaptiveTickRunner{
		inner:       inner,
		db:          db,
		adaptiveCap: nil,
	}

	mock.ExpectPing()
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM job_instances").WithArgs("t1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM workers").WithArgs("t1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	_, err = runner.SubmitNext(context.Background(), nil, "t1", "w1", 5, 60*time.Second, map[string]any{})
	if err != nil {
		t.Fatalf("SubmitNext() error = %v", err)
	}
	if inner.lastLimit != 5 {
		t.Fatalf("expected limit=5 when adaptiveCap is nil, got %d", inner.lastLimit)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestAdaptiveTickRunner_DynamicLeaseNil(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	inner := &capturingTickRunner{}

	runner := &adaptiveTickRunner{
		inner:        inner,
		db:           db,
		dynamicLease: nil,
	}

	mock.ExpectPing()
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM job_instances").WithArgs("t1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM workers").WithArgs("t1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	_, err = runner.SubmitNext(context.Background(), nil, "t1", "w1", 1, 120*time.Second, map[string]any{})
	if err != nil {
		t.Fatalf("SubmitNext() error = %v", err)
	}
	if inner.lastLease != 120*time.Second {
		t.Fatalf("expected lease=120s when dynamicLease is nil, got %s", inner.lastLease)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestAdaptiveTickRunner_DBProbeError(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	inner := &stubTickRunner{handled: 1}
	runner := &adaptiveTickRunner{
		inner: inner,
		db:    db,
	}

	mock.ExpectPing().WillReturnError(errors.New("ping boom"))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM job_instances").WithArgs("t1").WillReturnError(errors.New("query boom"))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM workers").WithArgs("t1").WillReturnError(errors.New("query boom"))

	n, err := runner.SubmitNext(context.Background(), nil, "t1", "w1", 1, 60*time.Second, map[string]any{})
	if err != nil {
		t.Fatalf("SubmitNext() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1, got %d", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

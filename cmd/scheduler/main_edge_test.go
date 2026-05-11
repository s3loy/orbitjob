package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/platform/config"
	"orbitjob/internal/platform/health"
)

// ---------------------------------------------------------------------------
// loadPositiveIntEnv edge cases
// ---------------------------------------------------------------------------

func TestLoadPositiveIntEnv_Zero(t *testing.T) {
	t.Setenv("TEST_SCHEDULER_KEY", "0")

	_, err := config.LoadPositiveIntEnv("TEST_SCHEDULER_KEY", 100)
	if err == nil || !strings.Contains(err.Error(), "must be >= 1") {
		t.Fatalf("expected 'must be >= 1' error, got %v", err)
	}
}

func TestLoadPositiveIntEnv_Negative(t *testing.T) {
	t.Setenv("TEST_SCHEDULER_KEY", "-5")

	_, err := config.LoadPositiveIntEnv("TEST_SCHEDULER_KEY", 100)
	if err == nil || !strings.Contains(err.Error(), "must be >= 1") {
		t.Fatalf("expected 'must be >= 1' error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// startComponentHealthServer
// ---------------------------------------------------------------------------

func TestStartComponentHealthServer_Healthz(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port := "19990"
	go health.StartComponentHealthServer(ctx, db, port, "scheduler")
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://localhost:" + port + "/healthz")
	if err != nil {
		t.Fatalf("healthz request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status=%d, got %d", http.StatusOK, resp.StatusCode)
	}
}

func TestStartComponentHealthServer_Readyz(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectPing()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port := "19991"
	go health.StartComponentHealthServer(ctx, db, port, "scheduler")
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://localhost:" + port + "/readyz")
	if err != nil {
		t.Fatalf("readyz request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status=%d, got %d", http.StatusOK, resp.StatusCode)
	}
}

func TestStartComponentHealthServer_ReadyzDBError(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectPing().WillReturnError(errors.New("db gone"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port := "19992"
	go health.StartComponentHealthServer(ctx, db, port, "scheduler")
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://localhost:" + port + "/readyz")
	if err != nil {
		t.Fatalf("readyz request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected status=%d, got %d", http.StatusServiceUnavailable, resp.StatusCode)
	}
}

func TestStartComponentHealthServer_Shutdown(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithCancel(context.Background())

	port := "19993"
	done := make(chan struct{})
	go func() {
		health.StartComponentHealthServer(ctx, db, port, "scheduler")
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://localhost:" + port + "/healthz")
	if err != nil {
		cancel()
		t.Fatalf("healthz request failed: %v", err)
	}
	_ = resp.Body.Close()

	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("startComponentHealthServer did not shutdown after context cancel")
	}
}

// ---------------------------------------------------------------------------
// run() config error path
// ---------------------------------------------------------------------------

func TestRun_ConfigError(t *testing.T) {
	resetSchedulerMainDeps(t)
	t.Setenv("DATABASE_DSN", "postgres://unit-test")
	t.Setenv("SCHEDULER_BATCH_SIZE", "bad")

	loadDotenvFn = func() error { return nil }
	newLoggerFn = func(string) *slog.Logger { return slog.Default() }

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "SCHEDULER_BATCH_SIZE") {
		t.Fatalf("expected SCHEDULER_BATCH_SIZE error, got %v", err)
	}
}

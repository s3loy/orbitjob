package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// Ports are distinct from the ones cmd/scheduler/main_edge_test.go uses, since
// package test binaries can run concurrently.
const (
	testPortHealthz  = "21337"
	testPortReadyz   = "21339"
	testPortReadyzDB = "21340"
	testPortMetrics  = "21341"
	testPortShutdown = "21343"
)

func sqlDBForTest(t *testing.T, pingErr error) *sql.DB {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if pingErr != nil {
		mock.ExpectPing().WillReturnError(pingErr)
	} else {
		mock.ExpectPing()
	}
	return db
}

// startServer runs the health server and waits until it answers /healthz, so
// no test depends on a fixed startup sleep.
func startServer(t *testing.T, db *sql.DB, port, component string) (<-chan struct{}, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		StartComponentHealthServer(ctx, db, port, component)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := http.Get("http://127.0.0.1:" + port + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("health server on port %s did not become responsive: %v", port, err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("health server did not stop after context cancel")
		}
	})
	return done, cancel
}

func getJSON(t *testing.T, url string) (int, http.Header, map[string]string) {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("body %q is not a JSON object: %v", body, err)
	}
	return resp.StatusCode, resp.Header.Clone(), payload
}

func TestHealthzReportsComponentStatus(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	startServer(t, db, testPortHealthz, "operator")

	status, header, payload := getJSON(t, "http://127.0.0.1:"+testPortHealthz+"/healthz")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if contentType := header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("content type = %q, want application/json", contentType)
	}
	if payload["status"] != "ok" {
		t.Fatalf("status field = %q, want ok", payload["status"])
	}
	if payload["component"] != "operator" {
		t.Fatalf("component field = %q, want operator", payload["component"])
	}
}

func TestReadyzReportsReadyWhenDatabasePings(t *testing.T) {
	db := sqlDBForTest(t, nil)

	startServer(t, db, testPortReadyz, "operator")

	status, _, payload := getJSON(t, "http://127.0.0.1:"+testPortReadyz+"/readyz")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if payload["status"] != "ready" {
		t.Fatalf("status field = %q, want ready", payload["status"])
	}
	if payload["component"] != "operator" {
		t.Fatalf("component field = %q, want operator", payload["component"])
	}
}

func TestReadyzReportsNotReadyWhenDatabaseUnreachable(t *testing.T) {
	db := sqlDBForTest(t, errors.New("db gone"))

	startServer(t, db, testPortReadyzDB, "operator")

	status, _, payload := getJSON(t, "http://127.0.0.1:"+testPortReadyzDB+"/readyz")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", status, http.StatusServiceUnavailable)
	}
	if payload["status"] != "not ready" {
		t.Fatalf("status field = %q, want not ready", payload["status"])
	}
	if payload["error"] == "" {
		t.Fatal("a failed readiness check must say why")
	}
}

func TestMetricsServesPrometheusExposition(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	startServer(t, db, testPortMetrics, "operator")

	resp, err := http.Get("http://127.0.0.1:" + testPortMetrics + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "# HELP") {
		t.Fatal("/metrics must serve Prometheus exposition format")
	}
}

func TestServerStopsWhenContextIsCancelled(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	done, cancel := startServer(t, db, testPortShutdown, "operator")
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("health server did not shut down after context cancel")
	}
}

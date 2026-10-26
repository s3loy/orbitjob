package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
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
func startServer(t *testing.T, db *sql.DB, component string) (string, <-chan struct{}, context.CancelFunc) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	baseURL := "http://" + listener.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		serveComponentHealthServer(ctx, db, listener, component)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := http.Get(baseURL + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("health server at %s did not become responsive: %v", baseURL, err)
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
	return baseURL, done, cancel
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

	baseURL, _, _ := startServer(t, db, "operator")

	status, header, payload := getJSON(t, baseURL+"/healthz")
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

	baseURL, _, _ := startServer(t, db, "operator")

	status, _, payload := getJSON(t, baseURL+"/readyz")
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

	baseURL, _, _ := startServer(t, db, "operator")

	status, _, payload := getJSON(t, baseURL+"/readyz")
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

	baseURL, _, _ := startServer(t, db, "operator")

	resp, err := http.Get(baseURL + "/metrics")
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

	_, done, cancel := startServer(t, db, "operator")
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("health server did not shut down after context cancel")
	}
}

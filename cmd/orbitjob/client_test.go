package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

// TestSuccessPathDecodesAndAuthenticates covers the happy path end to end:
// bearer header sent, JSON decoded, exit 0.
func TestSuccessPathDecodesAndAuthenticates(t *testing.T) {
	var auth string
	server, _ := testServerWith(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"items":[{"id":1,"name":"nightly","status":"active","check_type":"http_health","schedule_type":"cron","created_at":"2026-09-18T00:00:00Z"}]}`))
	})
	out := captureStdout(t, func() {
		_ = checksList(context.Background(), []string{"--api-url", server.URL, "--api-key", "k"})
	})
	if auth != "Bearer k" {
		t.Fatalf("Authorization = %q", auth)
	}
	if !strings.Contains(out, "nightly") || !strings.Contains(out, "active") {
		t.Fatalf("checks list output missing row content:\n%s", out)
	}
}

// TestStatusMappingOverHTTPTeful pins each API status to its exit code through
// the real client against a real HTTP server.
func TestStatusMappingOverHTTP(t *testing.T) {
	cases := []struct {
		status int
		code   string
		want   int
	}{
		{http.StatusNotFound, "NOT_FOUND", exitNotFound},
		{http.StatusForbidden, "FORBIDDEN", exitForbidden},
		{http.StatusUnauthorized, "UNAUTHORIZED", exitForbidden},
		{http.StatusInternalServerError, "INTERNAL_ERROR", exitGeneral},
		{http.StatusBadRequest, "VALIDATION_ERROR", exitGeneral},
		{http.StatusConflict, "CONFLICT", exitGeneral},
	}
	for _, tc := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"error":{"code":"` + tc.code + `","message":"detail"}}`))
		}))
		err := runsList(context.Background(), []string{"--api-url", server.URL, "--api-key", "k"})
		server.Close()
		if got := exitCodeOfErr(t, err); got != tc.want {
			t.Fatalf("%d %s -> exit %d, want %d", tc.status, tc.code, got, tc.want)
		}
	}
}

// TestConnectionRefusedGetsTheHint pins the port-forward hint and exit 1.
func TestConnectionRefusedGetsTheHint(t *testing.T) {
	// Port 1 on loopback is closed by construction; no server to race with.
	err := runsList(context.Background(), []string{"--api-url", "http://127.0.0.1:1", "--api-key", "k"})
	if err == nil {
		t.Fatal("connection refused must error")
	}
	if got := exitCodeOfErr(t, err); got != exitGeneral {
		t.Fatalf("connection refused exit = %d, want %d", got, exitGeneral)
	}
	if !strings.Contains(err.Error(), "[FAIL]") {
		t.Fatalf("connection refused must print a [FAIL] line: %v", err)
	}
	if !strings.Contains(err.Error(), "port-forward") {
		t.Fatalf("connection refused must hint at the port-forward: %v", err)
	}
}

// TestConnectionRefusedForCancel pins the same mapping on the mutating path.
func TestConnectionRefusedForCancel(t *testing.T) {
	err := runsCancel(context.Background(), []string{"7", "--api-url", "http://127.0.0.1:1", "--api-key", "k"})
	if got := exitCodeOfErr(t, err); got != exitGeneral {
		t.Fatalf("cancel connection refused exit = %d, want %d", got, exitGeneral)
	}
}

func TestIsConnectionErrorClassification(t *testing.T) {
	if !isConnectionError(&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}) {
		t.Fatal("ECONNREFUSED should classify as a connection error")
	}
	if !isConnectionError(fmtWrap(&net.DNSError{Err: "no such host"})) {
		t.Fatal("DNS failure should classify as a connection error")
	}
	if isConnectionError(context.Canceled) {
		t.Fatal("context.Canceled is not a connection error")
	}
	if isConnectionError(errors.New("some other failure")) {
		t.Fatal("unknown errors are not connection errors")
	}
	timeout := &url.Error{Op: "Get", URL: "http://127.0.0.1:1/x", Err: context.DeadlineExceeded}
	if !isConnectionError(timeout) {
		t.Fatal("timeout should classify as a connection error")
	}
}

func fmtWrap(err error) error { return &net.OpError{Op: "dial", Err: err} }

// TestServerErrorDoesNotLeakKey covers the whole error surface for key leaks.
func TestServerErrorDoesNotLeakKey(t *testing.T) {
	const key = "otj_super_secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"INTERNAL_ERROR","message":"db exploded"}}`))
	}))
	client := &apiClient{base: server.URL, key: key}
	_, err := client.do(context.Background(), http.MethodGet, "/api/v1/instances", nil, nil)
	if err == nil {
		t.Fatal("500 must error")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("error leaked the key: %v", err)
	}
	var apiErr *apiError
	if !errors.As(err, &apiErr) || apiErr.code != "INTERNAL_ERROR" {
		t.Fatalf("500 should parse into a typed apiError: %v", err)
	}
}

// TestEnvKeyLeakGuard proves the key value never appears in CLI output, even
// when the connection fails and the error carries detail.
func TestEnvKeyLeakGuard(t *testing.T) {
	const secretKey = "otj_env_secret_never_print"
	t.Setenv("ORBITJOB_API_KEY", secretKey)
	t.Setenv("ORBITJOB_API_URL", "http://127.0.0.1:1")
	t.Setenv("ORBITJOB_API", "")
	err := runsList(context.Background(), nil)
	if err == nil {
		t.Fatal("dead API URL must fail")
	}
	out := err.Error()
	if strings.Contains(out, secretKey) {
		t.Fatalf("env key leaked into output: %s", out)
	}
	if !strings.Contains(out, "[FAIL]") {
		t.Fatalf("expected the connection [FAIL] hint: %s", out)
	}
}

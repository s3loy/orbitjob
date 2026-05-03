package handler

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"orbitjob/internal/core/app/execute"
)

func makeHTTPTask(payload map[string]any) execute.AssignedTask {
	return execute.AssignedTask{
		InstanceID:     1,
		TenantID:       "default",
		HandlerType:    "http",
		HandlerPayload: payload,
		TimeoutSec:     10,
	}
}

// disableSSRF is a helper that disables URL and transport-level IP blocking for
// tests that use httptest.NewServer (which binds to 127.0.0.1).
func disableSSRF() func() {
	saveValidate := validateCallbackURL
	saveBlocked := isBlockedIP
	validateCallbackURL = func(_ string) error { return nil }
	isBlockedIP = func(_ net.IP) bool { return false }
	return func() {
		validateCallbackURL = saveValidate
		isBlockedIP = saveBlocked
	}
}

func TestHTTPHandler_Success2xx(t *testing.T) {
	defer disableSSRF()()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := NewHTTPHandler(srv.Client())
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url":    srv.URL,
		"method": "GET",
	}))
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.ErrorMsg)
	}
	if result.ResultCode != "200" {
		t.Fatalf("expected result_code=200, got %q", result.ResultCode)
	}
}

func TestHTTPHandler_Non2xx(t *testing.T) {
	defer disableSSRF()()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	h := NewHTTPHandler(srv.Client())
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": srv.URL,
	}))
	if result.Success {
		t.Fatal("expected failure for 500")
	}
	if result.ResultCode != "500" {
		t.Fatalf("expected result_code=500, got %q", result.ResultCode)
	}
	if result.ErrorMsg != "server error" {
		t.Fatalf("expected error_msg=%q, got %q", "server error", result.ErrorMsg)
	}
}

func TestHTTPHandler_DefaultMethodIsPOST(t *testing.T) {
	defer disableSSRF()()

	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := NewHTTPHandler(srv.Client())
	h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": srv.URL,
	}))
	if gotMethod != "POST" {
		t.Fatalf("expected default method=POST, got %q", gotMethod)
	}
}

func TestHTTPHandler_HeadersForwarded(t *testing.T) {
	defer disableSSRF()()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := NewHTTPHandler(srv.Client())
	h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url":     srv.URL,
		"headers": map[string]any{"Authorization": "Bearer token-123"},
	}))
	if gotAuth != "Bearer token-123" {
		t.Fatalf("expected Authorization header, got %q", gotAuth)
	}
}

func TestHTTPHandler_Timeout(t *testing.T) {
	defer disableSSRF()()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	h := NewHTTPHandler(srv.Client())
	result := h.Execute(ctx, makeHTTPTask(map[string]any{
		"url": srv.URL,
	}))
	if result.Success {
		t.Fatal("expected failure on timeout")
	}
	if result.ResultCode != "timeout" {
		t.Fatalf("expected result_code=timeout, got %q", result.ResultCode)
	}
}

func TestHTTPHandler_InvalidPayload_MissingURL(t *testing.T) {
	h := NewHTTPHandler(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{}))
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ResultCode != "invalid_payload" {
		t.Fatalf("expected result_code=invalid_payload, got %q", result.ResultCode)
	}
}

func TestHTTPHandler_InvalidPayload_BadHeaders(t *testing.T) {
	h := NewHTTPHandler(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url":     "http://localhost",
		"headers": "not-a-map",
	}))
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ResultCode != "invalid_payload" {
		t.Fatalf("expected result_code=invalid_payload, got %q", result.ResultCode)
	}
}

func TestHTTPHandler_ConnectionRefused(t *testing.T) {
	defer disableSSRF()()

	h := NewHTTPHandler(&http.Client{})
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "http://127.0.0.1:1",
	}))
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ResultCode != "error" {
		t.Fatalf("expected result_code=error, got %q", result.ResultCode)
	}
}

// ---------------------------------------------------------------------------
// SSRF protection tests
// ---------------------------------------------------------------------------

func TestHTTPHandler_SSRF_Private10(t *testing.T) {
	h := NewHTTPHandler(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "http://10.0.0.1/",
	}))
	if result.Success {
		t.Fatal("expected ssrf_blocked for 10.0.0.0/8")
	}
	if result.ResultCode != "ssrf_blocked" {
		t.Fatalf("expected result_code=ssrf_blocked, got %q", result.ResultCode)
	}
}

func TestHTTPHandler_SSRF_Private172(t *testing.T) {
	h := NewHTTPHandler(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "http://172.16.0.1/",
	}))
	if result.Success {
		t.Fatal("expected ssrf_blocked for 172.16.0.0/12")
	}
	if result.ResultCode != "ssrf_blocked" {
		t.Fatalf("expected result_code=ssrf_blocked, got %q", result.ResultCode)
	}
}

func TestHTTPHandler_SSRF_Private192(t *testing.T) {
	h := NewHTTPHandler(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "http://192.168.1.1/",
	}))
	if result.Success {
		t.Fatal("expected ssrf_blocked for 192.168.0.0/16")
	}
	if result.ResultCode != "ssrf_blocked" {
		t.Fatalf("expected result_code=ssrf_blocked, got %q", result.ResultCode)
	}
}

func TestHTTPHandler_SSRF_Loopback(t *testing.T) {
	h := NewHTTPHandler(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "http://127.0.0.1/",
	}))
	if result.Success {
		t.Fatal("expected ssrf_blocked for 127.0.0.0/8")
	}
	if result.ResultCode != "ssrf_blocked" {
		t.Fatalf("expected result_code=ssrf_blocked, got %q", result.ResultCode)
	}
}

func TestHTTPHandler_SSRF_Metadata(t *testing.T) {
	h := NewHTTPHandler(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "http://169.254.169.254/",
	}))
	if result.Success {
		t.Fatal("expected ssrf_blocked for metadata IP")
	}
	if result.ResultCode != "ssrf_blocked" {
		t.Fatalf("expected result_code=ssrf_blocked, got %q", result.ResultCode)
	}
}

func TestHTTPHandler_SSRF_BadScheme(t *testing.T) {
	h := NewHTTPHandler(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "ftp://example.com/",
	}))
	if result.Success {
		t.Fatal("expected ssrf_blocked for non-http scheme")
	}
	if result.ResultCode != "ssrf_blocked" {
		t.Fatalf("expected result_code=ssrf_blocked, got %q", result.ResultCode)
	}
}

func TestHTTPHandler_SSRF_RedirectDisabled(t *testing.T) {
	defer disableSSRF()()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/target", http.StatusMovedPermanently)
	}))
	defer srv.Close()

	h := NewHTTPHandler(srv.Client())
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": srv.URL,
	}))
	if result.Success {
		t.Fatal("expected non-2xx (301) since redirects are disabled")
	}
	if result.ResultCode != "301" {
		t.Fatalf("expected result_code=301 (redirect not followed), got %q", result.ResultCode)
	}
}

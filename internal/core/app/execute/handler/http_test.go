package handler

import (
	"context"
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

func TestHTTPHandler_Success2xx(t *testing.T) {
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
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

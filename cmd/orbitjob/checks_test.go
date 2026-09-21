package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChecksGetRendersSchedule(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"id": 9, "name": "login-probe", "status": "active", "check_type": "http_health",
			"schedule_type": "cron", "cron_expr": "*/5 * * * *",
			"timeout_sec": 10, "retry_limit": 1, "priority": 5,
			"created_at": "2026-09-17T00:00:00Z", "updated_at": "2026-09-18T00:00:00Z"
		}`))
	}))
	defer server.Close()

	out := captureStdout(t, func() {
		_ = checksGet(context.Background(), []string{"9", "--api-url", server.URL, "--api-key", "k"})
	})
	for _, want := range []string{"name: login-probe", "status: active", "schedule: cron: */5 * * * *", "timeout_sec: 10"} {
		if !strings.Contains(out, want) {
			t.Fatalf("checks get output missing %q:\n%s", want, out)
		}
	}
}

func TestChecksGetIntervalSchedule(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id": 10, "name": "probe", "status": "paused", "check_type": "http_health", "schedule_type": "interval", "interval_sec": 60, "created_at": "2026-09-17T00:00:00Z"}`))
	}))
	defer server.Close()

	out := captureStdout(t, func() {
		_ = checksGet(context.Background(), []string{"10", "--api-url", server.URL, "--api-key", "k"})
	})
	if !strings.Contains(out, "schedule: interval: 60s") {
		t.Fatalf("interval schedule not rendered:\n%s", out)
	}
}

func TestChecksListTable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeItems(t, w, []map[string]any{
			{"id": 9, "name": "login-probe", "status": "active", "check_type": "http_health", "schedule_type": "cron", "created_at": "2026-09-17T00:00:00Z"},
		})
	}))
	defer server.Close()

	out := captureStdout(t, func() {
		_ = checksList(context.Background(), []string{"--api-url", server.URL, "--api-key", "k", "--status", "active"})
	})
	if !strings.Contains(out, "login-probe") || !strings.Contains(out, "http_health") {
		t.Fatalf("checks list output wrong:\n%s", out)
	}
}

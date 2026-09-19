package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJobsListTable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeItems(t, w, []map[string]any{
			{"id": 3, "name": "nightly-index", "namespace": "orbitjob-tasks", "schedule": "0 2 * * *", "suspend": false, "schedule_summary": "cron: 0 2 * * *", "created_at": "2026-09-17T00:00:00Z"},
			{"id": 4, "name": "paused-job", "namespace": "orbitjob-tasks", "schedule": "0 3 * * *", "suspend": true, "schedule_summary": "suspended", "created_at": "2026-09-17T00:00:00Z"},
		})
	}))
	defer server.Close()

	out := captureStdout(t, func() {
		_ = jobsList(context.Background(), []string{"--api-url", server.URL, "--api-key", "k"})
	})
	if !strings.Contains(out, "nightly-index") || !strings.Contains(out, "cron: 0 2 * * *") || !strings.Contains(out, "suspended") {
		t.Fatalf("jobs list output wrong:\n%s", out)
	}
}

func TestJobsGetRendersSpec(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"id": 3, "name": "nightly-index", "namespace": "orbitjob-tasks",
			"schedule": "0 2 * * *", "suspend": false, "schedule_summary": "cron: 0 2 * * *",
			"timeout_seconds": 600, "retry_max_attempts": 3, "actor": "declarer",
			"created_at": "2026-09-17T00:00:00Z",
			"job_template": {"image": "registry.example/indexer:1.4", "backoff_limit": 2}
		}`))
	}))
	defer server.Close()

	out := captureStdout(t, func() {
		_ = jobsGet(context.Background(), []string{"3", "--api-url", server.URL, "--api-key", "k"})
	})
	for _, want := range []string{
		"name: nightly-index", "schedule: cron: 0 2 * * *", "timeout_seconds: 600",
		"retry_max_attempts: 3", "image: registry.example/indexer:1.4", "suspend: false",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("jobs get output missing %q:\n%s", want, out)
		}
	}
}

// TestJobsTriggerSendsIdempotencyKeyAndAcceptsCreated pins the header and the
// 201 path.
func TestJobsTriggerSendsIdempotencyKeyAndAcceptsCreated(t *testing.T) {
	var idem string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idem = r.Header.Get("X-OrbitJob-Idempotency-Key")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"namespace":"orbitjob-tasks","name":"jr-2","occurrence_key":"manual-x","trigger":"manual","phase":"Pending","created":true}`))
	}))
	defer server.Close()

	out := captureStdout(t, func() {
		_ = jobsTrigger(context.Background(), []string{"3", "--idempotency-key", "burst-2026", "--api-url", server.URL, "--api-key", "k"})
	})
	if idem != "burst-2026" {
		t.Fatalf("idempotency header = %q", idem)
	}
	if !strings.Contains(out, "[OK] triggered job 3") || !strings.Contains(out, "phase Pending") {
		t.Fatalf("trigger output wrong:\n%s", out)
	}
}

func TestJobsTriggerWithoutIdempotencyOmitsHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-OrbitJob-Idempotency-Key") != "" {
			t.Errorf("idempotency header sent without the flag")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"namespace":"n","name":"j","occurrence_key":"k","phase":"Pending"}`))
	}))
	defer server.Close()

	_ = captureStdout(t, func() {
		_ = jobsTrigger(context.Background(), []string{"3", "--api-url", server.URL, "--api-key", "k"})
	})
}

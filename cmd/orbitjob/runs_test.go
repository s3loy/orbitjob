package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRunsGetRendersAttemptTrail covers the detail view and its sub-table.
func TestRunsGetRendersAttemptTrail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/instances/42" {
			t.Errorf("path = %s, want /api/v1/instances/42", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{
			"id": 42, "phase": "RetryWaiting", "occurrence_key": "occ-1",
			"trigger": "schedule", "actor": "operator",
			"attempt": 2, "max_attempts": 3,
			"created_at": "2026-09-18T01:00:00Z", "updated_at": "2026-09-18T01:05:00Z",
			"attempts": [
				{"id": 1, "attempt_number": 1, "phase": "Failed", "kubernetes_job_name": "job-a1", "started_at": "2026-09-18T01:00:00Z", "completed_at": "2026-09-18T01:02:00Z"},
				{"id": 2, "attempt_number": 2, "phase": "Running", "kubernetes_job_name": "job-a2"}
			]
		}`))
	}))
	defer server.Close()

	out := captureStdout(t, func() {
		_ = runsGet(context.Background(), []string{"42", "--api-url", server.URL, "--api-key", "k"})
	})
	for _, want := range []string{
		"id: 42", "phase: RetryWaiting", "attempts: 2/3", "occurrence_key: occ-1",
		"ATTEMPT", "job-a1", "Failed", "job-a2", "-",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("runs get output missing %q:\n%s", want, out)
		}
	}
}

// TestRunsCancelPostsNoBodyToTheCancelRoute pins the real route and the
// body-less contract.
func TestRunsCancelPostsNoBodyToTheCancelRoute(t *testing.T) {
	var method, path string
	var bodyLen int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		buf := make([]byte, 64)
		for {
			n, err := r.Body.Read(buf)
			bodyLen += n
			if err != nil {
				break
			}
		}
		_, _ = w.Write([]byte(`{"namespace":"orbitjob-tasks","name":"jr-1","occurrence_key":"occ-9","phase":"Running"}`))
	}))
	defer server.Close()

	out := captureStdout(t, func() {
		err := runsCancel(context.Background(), []string{"7", "--api-url", server.URL, "--api-key", "k"})
		if err != nil {
			t.Errorf("cancel: %v", err)
		}
	})
	if method != http.MethodPost || path != "/api/v1/instances/7/cancel" {
		t.Fatalf("cancel called %s %s, want POST /api/v1/instances/7/cancel", method, path)
	}
	if bodyLen != 0 {
		t.Fatalf("cancel must send no body, sent %d bytes", bodyLen)
	}
	if !strings.Contains(out, "[OK] cancel requested for run 7") || !strings.Contains(out, "phase at request: Running") {
		t.Fatalf("cancel output wrong:\n%s", out)
	}
}

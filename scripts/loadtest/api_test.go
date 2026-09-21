package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTriggerJobSendsIdempotencyKey(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-OrbitJob-Idempotency-Key")
		if gotTenant := r.Header.Get("X-OrbitJob-Tenant"); gotTenant != "" {
			t.Errorf("tenant header %q sent, but tenancy comes from the API key", gotTenant)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"namespace":"orbitjob-tasks","name":"case-1-abcd1234","occurrence_key":"abcd1234ef567890","trigger":"Manual","phase":"","created":true}`)
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "tenant-key")
	resp, err := c.TriggerJob(context.Background(), 7, "case-7")
	if err != nil {
		t.Fatal(err)
	}
	if gotHeader != "case-7" {
		t.Fatalf("idempotency key = %q", gotHeader)
	}
	if !resp.Created || resp.OccurrenceKey != "abcd1234ef567890" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestTriggerJobConflictMeansNoNewRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "tenant-key")
	resp, err := c.TriggerJob(context.Background(), 7, "case-7")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Created {
		t.Fatal("conflict should not create a run")
	}
}

func TestTriggerJobRateLimitReturnsTypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "tenant-key")
	_, err := c.TriggerJob(context.Background(), 7, "case-7")
	var terr *TriggerError
	if !errors.As(err, &terr) {
		t.Fatalf("error type = %T, want *TriggerError", err)
	}
	if terr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", terr.StatusCode)
	}
}

func TestTriggerJobTransportErrorHasZeroStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // close before use so dials fail

	c := NewAPIClient(srv.URL, "tenant-key")
	_, err := c.TriggerJob(context.Background(), 7, "case-7")
	var terr *TriggerError
	if !errors.As(err, &terr) {
		t.Fatalf("error type = %T, want *TriggerError", err)
	}
	if terr.StatusCode != 0 {
		t.Fatalf("transport error status = %d, want 0", terr.StatusCode)
	}
}

func TestCancelInstanceSendsNoBody(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		buf := make([]byte, 1)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "tenant-key")
	if err := c.CancelInstance(context.Background(), "42"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/instances/42/cancel" {
		t.Fatalf("path = %q", gotPath)
	}
	// The cancel request carries no body: the whole effect is the
	// spec.cancelRequested patch the API performs.
	if gotBody != "" {
		t.Fatalf("cancel sent a body %q, want none", gotBody)
	}
}

func TestCancelInstanceConflictIsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "tenant-key")
	err := c.CancelInstance(context.Background(), "42")
	var cerr *CancelError
	if !errors.As(err, &cerr) {
		t.Fatalf("error type = %T, want *CancelError", err)
	}
	if cerr.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (JobRun CR missing)", cerr.StatusCode)
	}
}

func TestRunIDByOccurrenceKeyPollsUntilTheRowAppears(t *testing.T) {
	// The operator writes the ledger row asynchronously, so the first list
	// legitimately misses. The resolver keeps polling until the key shows up.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls < 3 {
			_, _ = fmt.Fprint(w, `{"items":[]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"items":[{"id":41,"occurrence_key":"another-key"},{"id":42,"occurrence_key":"abcd1234ef567890"}]}`)
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "tenant-key")
	runID, err := c.RunIDByOccurrenceKey(context.Background(), "abcd1234ef567890", time.Now().Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if runID != "42" {
		t.Fatalf("run id = %q, want 42", runID)
	}
}

func TestRunIDByOccurrenceKeyGivesUpAtTheDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"items":[{"id":41,"occurrence_key":"another-key"}]}`)
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "tenant-key")
	_, err := c.RunIDByOccurrenceKey(context.Background(), "never-appears", time.Now().Add(2*time.Second))
	if err == nil || !strings.Contains(err.Error(), "did not appear") {
		t.Fatalf("error = %v, want a missing-row error", err)
	}
}

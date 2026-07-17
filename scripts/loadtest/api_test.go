package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTriggerJobSendsIdempotencyKey(t *testing.T) {
	var gotHeader, gotTenant string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-OrbitJob-Idempotency-Key")
		gotTenant = r.Header.Get("X-OrbitJob-Tenant")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"run_id":"run-41","job_id":7,"tenant_id":"load-alpha","status":"pending","created":true}`)
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "tenant-key")
	resp, err := c.TriggerJob(context.Background(), 7, "load-alpha", "v020-case-7")
	if err != nil {
		t.Fatal(err)
	}
	if gotHeader != "v020-case-7" {
		t.Fatalf("idempotency key = %q", gotHeader)
	}
	if gotTenant != "load-alpha" {
		t.Fatalf("tenant = %q", gotTenant)
	}
	if !resp.Created || resp.RunID != "run-41" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestTriggerJobConflictMeansNoNewInstance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "tenant-key")
	resp, err := c.TriggerJob(context.Background(), 7, "load-alpha", "v020-case-7")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Created {
		t.Fatal("conflict should not create instance")
	}
}

func TestCreateJobRequiresCreatedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "tenant-key")
	_, err := c.CreateJob(context.Background(), "load-alpha", map[string]any{"name": "x"})
	if err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("error = %v", err)
	}
}

func TestTriggerJobRateLimitReturnsTypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "tenant-key")
	_, err := c.TriggerJob(context.Background(), 7, "load-alpha", "v020-case-7")
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
	_, err := c.TriggerJob(context.Background(), 7, "load-alpha", "v020-case-7")
	var terr *TriggerError
	if !errors.As(err, &terr) {
		t.Fatalf("error type = %T, want *TriggerError", err)
	}
	if terr.StatusCode != 0 {
		t.Fatalf("transport error status = %d, want 0", terr.StatusCode)
	}
}

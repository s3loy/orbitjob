package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFlagBeatsEnvBeatsDefaultForURL pins the configuration precedence: the
// --api-url flag wins over both environment spellings, which win over the
// built-in default.
func TestFlagBeatsEnvBeatsDefaultForURL(t *testing.T) {
	t.Setenv("ORBITJOB_API_URL", "http://canonical-env:1")
	t.Setenv("ORBITJOB_API", "http://kind-env:2")

	fsConn := connFlags{apiURL: "http://flag:3", apiKey: "k"}
	client, err := fsConn.resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if client.base != "http://flag:3" {
		t.Fatalf("flag --api-url lost: base = %s", client.base)
	}

	envConn := connFlags{apiKey: "k"}
	client, err = envConn.resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if client.base != "http://canonical-env:1" {
		t.Fatalf("ORBITJOB_API_URL should beat ORBITJOB_API: base = %s", client.base)
	}

	t.Setenv("ORBITJOB_API_URL", "")
	onlyKind := connFlags{apiKey: "k"}
	client, err = onlyKind.resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if client.base != "http://kind-env:2" {
		t.Fatalf("ORBITJOB_API fallback lost: base = %s", client.base)
	}

	t.Setenv("ORBITJOB_API", "")
	bare := connFlags{apiKey: "k"}
	client, err = bare.resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if client.base != DefaultAPIURL {
		t.Fatalf("default lost: base = %s, want %s", client.base, DefaultAPIURL)
	}
}

// TestKeyFlagBeatsEnv pins the key precedence and that a missing key is a
// spoken error rather than a naked request.
func TestKeyFlagBeatsEnv(t *testing.T) {
	t.Setenv("ORBITJOB_API_KEY", "env-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer flag-key" {
			t.Errorf("Authorization = %q, want the flag key", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()

	out := captureStdout(t, func() {
		_ = runsList(context.Background(), []string{"--api-url", server.URL, "--api-key", "flag-key"})
	})
	_ = out

	t.Setenv("ORBITJOB_API_KEY", "")
	err := runsList(context.Background(), []string{"--api-url", server.URL})
	if got := exitCodeOfErr(t, err); got != exitGeneral {
		t.Fatalf("missing key exit = %d, want %d", got, exitGeneral)
	}
	if !strings.Contains(err.Error(), "ORBITJOB_API_KEY") {
		t.Fatalf("missing key error should name the env var: %v", err)
	}
}

// TestEnvKeyReachesServer proves the environment key alone authenticates.
func TestEnvKeyReachesServer(t *testing.T) {
	t.Setenv("ORBITJOB_API_KEY", "env-only-key")
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()

	_ = captureStdout(t, func() {
		_ = runsList(context.Background(), []string{"--api-url", server.URL})
	})
	if gotAuth != "Bearer env-only-key" {
		t.Fatalf("env key not sent: Authorization = %q", gotAuth)
	}
}

// TestResolveKeyReportsSourceWithoutValue pins doctor's promise: it says where
// the key came from and never the key itself.
func TestResolveKeyReportsSourceWithoutValue(t *testing.T) {
	if src := resolveKey("flag-value"); src != keyFromFlag {
		t.Fatalf("flag key source = %s", src)
	}
	t.Setenv("ORBITJOB_API_KEY", "env-value")
	if src := resolveKey(""); src != keyFromEnv {
		t.Fatalf("env key source = %s", src)
	}
	t.Setenv("ORBITJOB_API_KEY", "")
	if src := resolveKey(""); src != keyFromSecret {
		t.Fatalf("no flag no env should be secret, got %s", src)
	}
}

func TestPositiveID(t *testing.T) {
	usageLine := "orbitjob runs get ID"
	if id, err := positiveID("42", "run id", usageLine); err != nil || id != 42 {
		t.Fatalf("positiveID(42) = %d, %v", id, err)
	}
	for _, bad := range []string{"0", "-1", "abc", ""} {
		if _, err := positiveID(bad, "run id", usageLine); err == nil {
			t.Fatalf("positiveID(%q) accepted", bad)
		}
	}
}

func TestEmptyListPrintsQuietLine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()
	out := captureStdout(t, func() {
		_ = runsList(context.Background(), []string{"--api-url", server.URL, "--api-key", "k"})
	})
	if strings.TrimSpace(out) != "no runs" {
		t.Fatalf("empty list output = %q, want %q", out, "no runs")
	}
}

func TestListSendsQueryParameters(t *testing.T) {
	var gotQuery, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()

	_ = captureStdout(t, func() {
		_ = runsList(context.Background(), []string{"--api-url", server.URL, "--api-key", "k", "--phase", "Running", "--limit", "10", "--offset", "5"})
	})
	if gotPath != "/api/v1/instances" {
		t.Fatalf("path = %s", gotPath)
	}
	for _, want := range []string{"phase=Running", "limit=10", "offset=5"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query %q missing %s", gotQuery, want)
		}
	}
}

func TestDefaultsOmittedFromQuery(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()

	_ = captureStdout(t, func() {
		_ = checksList(context.Background(), []string{"--api-url", server.URL, "--api-key", "k"})
	})
	if gotQuery != "" {
		t.Fatalf("default flags should be omitted, got query %q", gotQuery)
	}
}

func TestErrorMessageCarriesServerWords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"NOT_FOUND","message":"run 999 not found"}}`))
	}))
	defer server.Close()

	err := runsGet(context.Background(), []string{"999", "--api-url", server.URL, "--api-key", "k"})
	if err == nil {
		t.Fatal("404 must surface as an error")
	}
	if !strings.Contains(err.Error(), "run 999 not found") {
		t.Fatalf("error should carry the server message: %v", err)
	}
	if got := exitCodeOfErr(t, err); got != exitNotFound {
		t.Fatalf("404 exit = %d, want %d", got, exitNotFound)
	}
}

func TestClientNeverPrintsKey(t *testing.T) {
	const secretKey = "otj_never_print_me"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"FORBIDDEN","message":"tenant mismatch"}}`))
	}))
	defer server.Close()

	err := runsList(context.Background(), []string{"--api-url", server.URL, "--api-key", secretKey})
	if err == nil {
		t.Fatal("403 must surface as an error")
	}
	if strings.Contains(err.Error(), secretKey) {
		t.Fatalf("error message leaked the key: %v", err)
	}
}

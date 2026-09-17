package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

// The binary's whole surface is main(), which os.Exits on every path. The
// tests re-execute the compiled test binary in a mode where TestMain calls
// main() with the URL under test, so the real exit codes are observed.
const (
	runMainEnv = "HEALTHCHECK_RUN_MAIN_FOR_TEST"
	urlEnv     = "HEALTHCHECK_TEST_URL"
)

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		args := []string{os.Args[0]}
		if url := os.Getenv(urlEnv); url != "" {
			args = append(args, url)
		}
		os.Args = args
		main() // every path of main exits the process
	}
	os.Exit(m.Run())
}

// runHealthcheck executes the binary against target and returns its exit code.
func runHealthcheck(t *testing.T, target string) int {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), runMainEnv+"=1", urlEnv+"="+target)

	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exitErr):
		return exitErr.ExitCode()
	default:
		t.Fatalf("run healthcheck: %v", err)
		return -1
	}
}

func TestHealthcheck_NoArgumentExitsOne(t *testing.T) {
	if code := runHealthcheck(t, ""); code != 1 {
		t.Fatalf("exit code without a url argument = %d, want 1", code)
	}
}

func TestHealthcheck_SuccessfulResponseExitsZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if code := runHealthcheck(t, server.URL); code != 0 {
		t.Fatalf("exit code for a 200 response = %d, want 0", code)
	}
}

func TestHealthcheck_RedirectIsNotHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	if code := runHealthcheck(t, server.URL); code != 1 {
		t.Fatalf("exit code for a 302 response = %d, want 1", code)
	}
}

func TestHealthcheck_ClientErrorExitsOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	if code := runHealthcheck(t, server.URL); code != 1 {
		t.Fatalf("exit code for a 404 response = %d, want 1", code)
	}
}

func TestHealthcheck_ServerErrorExitsOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	if code := runHealthcheck(t, server.URL); code != 1 {
		t.Fatalf("exit code for a 500 response = %d, want 1", code)
	}
}

func TestHealthcheck_UnreachableTargetExitsOne(t *testing.T) {
	// Closing the server first guarantees nothing listens on the address.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	server.Close()

	if code := runHealthcheck(t, server.URL); code != 1 {
		t.Fatalf("exit code for a connection failure = %d, want 1", code)
	}
}

func TestHealthcheck_TimedOutTargetExitsOne(t *testing.T) {
	// The client timeout is 3 seconds; a handler that never answers must fail
	// the check rather than hang the probe. The handler watches its request
	// context so it stops when the probe disconnects, instead of making
	// server.Close() wait out the full sleep.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer server.Close()

	if code := runHealthcheck(t, server.URL); code != 1 {
		t.Fatalf("exit code for a timed-out endpoint = %d, want 1", code)
	}
}

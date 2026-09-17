package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureStdout swaps the package's stdout seam for a buffer.
func captureStdout(t *testing.T, body func()) string {
	t.Helper()
	original := stdoutWriter
	buf := &bytes.Buffer{}
	stdoutWriter = buf
	defer func() { stdoutWriter = original }()
	body()
	return buf.String()
}

// exitCodeOfErr reports the exit code run() would apply to a command error.
func exitCodeOfErr(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var e *exitError
	if errors.As(err, &e) {
		return e.code
	}
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return apiErr.exitCode()
	}
	return exitGeneral
}

// testServerWith mounts handler and returns the server and a client pointed
// at it with a fixed key tests can assert on.
func testServerWith(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *apiClient) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, &apiClient{base: server.URL, key: "otj_test_key"}
}

func writeItems(t *testing.T, w http.ResponseWriter, items []map[string]any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"items": items}); err != nil {
		t.Fatalf("encode items: %v", err)
	}
}

func TestExitCodeOfMapsTypedErrors(t *testing.T) {
	if got := exitCodeOfErr(t, &apiError{status: 404, code: "NOT_FOUND", message: "nope"}); got != exitNotFound {
		t.Fatalf("404 -> exit %d, want %d", got, exitNotFound)
	}
	if got := exitCodeOfErr(t, &apiError{status: 403, code: "FORBIDDEN", message: "denied"}); got != exitForbidden {
		t.Fatalf("403 -> exit %d, want %d", got, exitForbidden)
	}
	if got := exitCodeOfErr(t, &apiError{status: 401, code: "UNAUTHORIZED", message: "bad key"}); got != exitForbidden {
		t.Fatalf("401 -> exit %d, want %d", got, exitForbidden)
	}
	if got := exitCodeOfErr(t, &apiError{status: 500, code: "INTERNAL_ERROR", message: "boom"}); got != exitGeneral {
		t.Fatalf("500 -> exit %d, want %d", got, exitGeneral)
	}
	if got := exitCodeOfErr(t, errors.New("ordinary")); got != exitGeneral {
		t.Fatalf("ordinary error -> exit %d, want %d", got, exitGeneral)
	}
	if got := exitCodeOfErr(t, failf(1, "usage")); got != 1 {
		t.Fatalf("usage error -> exit %d, want 1", got)
	}
}

func TestUnknownCommandExitsOne(t *testing.T) {
	if code := run(context.Background(), []string{"frobnicate"}); code != 1 {
		t.Fatalf("unknown command exit = %d, want 1", code)
	}
}

func TestNoArgumentsPrintsUsage(t *testing.T) {
	if code := run(context.Background(), nil); code != 1 {
		t.Fatalf("no arguments exit = %d, want 1", code)
	}
}

func TestSubcommandHelpExitsZero(t *testing.T) {
	for _, args := range [][]string{
		{"runs", "list", "-h"},
		{"runs", "cancel", "-h"},
		{"jobs", "get", "-h"},
		{"jobs", "trigger", "-h"},
		{"checks", "list", "-h"},
	} {
		err := commands[args[0]](context.Background(), args[1:])
		if err != nil {
			t.Fatalf("%v: help returned %v, want nil", args, err)
		}
	}
}

func TestSplitPositionalOrdersIDAndFlags(t *testing.T) {
	pos, flags := splitPositional([]string{"7", "--json"})
	if len(pos) != 1 || pos[0] != "7" || len(flags) != 1 || flags[0] != "--json" {
		t.Fatalf("splitPositional(7 --json) = %v, %v", pos, flags)
	}
	pos, flags = splitPositional([]string{"--api-key", "k", "42"})
	if len(pos) != 1 || pos[0] != "42" || len(flags) != 2 {
		t.Fatalf("splitPositional leading flags = %v, %v", pos, flags)
	}
	pos, _ = splitPositional(nil)
	if len(pos) != 0 {
		t.Fatalf("empty args should give no positionals, got %v", pos)
	}
}

func TestRunsGetRejectsNonNumericID(t *testing.T) {
	err := runsGet(context.Background(), []string{"abc"})
	if got := exitCodeOfErr(t, err); got != exitGeneral {
		t.Fatalf("non-numeric id exit = %d, want %d", got, exitGeneral)
	}
	if err == nil || !strings.Contains(err.Error(), "positive integer") {
		t.Fatalf("non-numeric id error = %v, want usage guidance", err)
	}
}

func TestJSONOutputIsTheRawServerBody(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":42,"phase":"Running","occurrence_key":"k1","trigger":"manual","actor":"a","attempt":2,"max_attempts":3,"updated_at":"2026-09-18T00:00:00Z"}]}`))
	}
	server, _ := testServerWith(t, handler)
	out := captureStdout(t, func() {
		_ = runsList(context.Background(), []string{"--api-url", server.URL, "--api-key", "k", "--json"})
	})
	if !strings.Contains(out, `"occurrence_key":"k1"`) {
		t.Fatalf("--json output lost the raw body:\n%s", out)
	}
	if strings.Contains(out, "PHASE") {
		t.Fatalf("--json output must not render a table:\n%s", out)
	}
}

func TestTableOutputIsGreppable(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		writeItems(t, w, []map[string]any{
			{"id": 42, "phase": "Running", "occurrence_key": "k1", "trigger": "manual", "attempt": 2, "max_attempts": 3, "updated_at": "2026-09-18T00:00:00Z"},
		})
	}
	server, _ := testServerWith(t, handler)
	out := captureStdout(t, func() {
		_ = runsList(context.Background(), []string{"--api-url", server.URL, "--api-key", "k"})
	})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("table = 3 lines (header, separator, row), got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "PHASE") || !strings.Contains(lines[2], "Running") {
		t.Fatalf("table missing expected content:\n%s", out)
	}
}

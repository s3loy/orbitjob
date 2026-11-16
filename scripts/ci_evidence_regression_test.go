package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestBenchmarkTargetRejectsFailedOrEmptyMeasurements(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		status       int
		wantSuccess  bool
	}{
		{"failed after a measurement", "BenchmarkProbe-4 10 123 ns/op\nFAIL\n", 1, false},
		{"no measurements", "ok probe [no tests to run]\n", 0, false},
		{"valid measurement", "BenchmarkProbe-4 10 123 ns/op\nPASS\n", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := makeGateFixture(t)
			stub := "#!/bin/sh\nprintf '%s' \"$BENCH_OUTPUT\"\nexit \"$BENCH_STATUS\"\n"
			writeGateExecutable(t, filepath.Join(dir, "fakebin", "go"), stub)
			if err := os.WriteFile(filepath.Join(dir, "bench.txt"), []byte("stale benchmark"), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("make", "--no-print-directory", "bench", "APP_VERSION=0.0.0")
			cmd.Dir = dir
			cmd.Env = append(gateEnvironment("PATH", "BENCH_OUTPUT", "BENCH_STATUS"),
				"PATH="+filepath.Join(dir, "fakebin")+":"+os.Getenv("PATH"),
				"BENCH_OUTPUT="+tc.output, "BENCH_STATUS="+strconv.Itoa(tc.status))
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.wantSuccess {
				t.Fatalf("success=%v, want %v:\n%s", err == nil, tc.wantSuccess, out)
			}
			measurement, readErr := os.ReadFile(filepath.Join(dir, "bench.txt"))
			if tc.wantSuccess {
				if readErr != nil || string(measurement) != tc.output {
					t.Fatalf("published measurement=%q, err=%v", measurement, readErr)
				}
			} else if !os.IsNotExist(readErr) {
				t.Fatalf("failed benchmark left a publishable result: %q, err=%v", measurement, readErr)
			}
		})
	}
}

func TestIntegrationTargetRequiresDatabaseAndRunsSerially(t *testing.T) {
	for _, configured := range []bool{false, true} {
		dir := makeGateFixture(t)
		writeGateExecutable(t, filepath.Join(dir, "fakebin", "go"), "#!/bin/sh\nprintf '%s\\n' \"$*\" > go-called\n")
		cmd := exec.Command("make", "--no-print-directory", "integration", "APP_VERSION=0.0.0")
		cmd.Dir = dir
		dsn := ""
		if configured {
			dsn = "postgres://integration-fixture"
		}
		cmd.Env = append(gateEnvironment("PATH", "TEST_DATABASE_DSN"),
			"PATH="+filepath.Join(dir, "fakebin")+":"+os.Getenv("PATH"), "TEST_DATABASE_DSN="+dsn)
		out, err := cmd.CombinedOutput()
		if (err == nil) != configured {
			t.Fatalf("configured=%v success=%v:\n%s", configured, err == nil, out)
		}
		called, readErr := os.ReadFile(filepath.Join(dir, "go-called"))
		if !configured {
			if !os.IsNotExist(readErr) || !strings.Contains(string(out), "TEST_DATABASE_DSN is required") {
				t.Fatalf("missing database was not rejected before tests: called=%q err=%v output=%s", called, readErr, out)
			}
		} else {
			want := "test -p=1 -count=1 -tags integration ./db/migrations ./internal/platform/postgrestest ./internal/admin/bootstrap ./internal/admin/http ./internal/admin/store/postgres ./internal/core/store/postgres ./scripts/loadtest ./deploy/kind/console-fixture"
			if readErr != nil || strings.TrimSpace(string(called)) != want {
				t.Fatalf("integration did not run the required serial uncached packages: %q err=%v", called, readErr)
			}
		}
	}
}

func TestSmokeHTTPDoesNotAcceptIncompleteSuccessfulResponse(t *testing.T) {
	for _, transferFails := range []bool{false, true} {
		dir := t.TempDir()
		status := "0"
		if transferFails {
			status = "18"
		}
		writeGateExecutable(t, filepath.Join(dir, "fakebin", "curl"), "#!/bin/sh\nprintf '{\"phase\":\"Succeeded\"}\\n200'\nexit "+status+"\n")
		script := smokePrefix(t) + "\nhttp GET /probe\nassert_code 200 probe\nprintf 'COUNTS=%s:%s\\n' \"$PASS\" \"$FAIL\"\n"
		out, err := runSmokeFragment(t, dir, script, "PATH="+filepath.Join(dir, "fakebin")+":"+os.Getenv("PATH"))
		want := "COUNTS=1:0"
		if transferFails {
			want = "COUNTS=0:1"
		}
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("transferFails=%v expected %s, err=%v:\n%s", transferFails, want, err, out)
		}
	}
}

func TestSmokeCancellationRequiresObservedRunningAndCanceled(t *testing.T) {
	source := readGateSource(t, "smoke-test.sh")
	start := strings.Index(source, "# The cancel request carries no body:")
	end := strings.Index(source, "log_section \"8. Checks\"")
	if start < 0 || end <= start {
		t.Fatal("cannot locate cancellation scenario")
	}
	for _, tc := range []struct{ name, appear, running, final string }{
		{"missing run", "", "", ""},
		{"prematurely terminal", "1 Succeeded", "", ""},
		{"never running", "1 Pending", "Pending", ""},
		{"terminal before running", "1 Pending", "Succeeded", ""},
		{"wrong final", "1 Running", "Running", "Failed"},
		{"canceled", "1 Running", "Running", "Canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			script := smokePrefix(t) + `
BOOTSTRAP_KEY=fixture
SLEEP_REVISION_ID=1
http() { LAST_CODE=201; LAST_BODY='{"occurrence_key":"fixture"}'; if [[ $2 == */cancel ]]; then LAST_CODE=200; fi; }
wait_run_appear() { printf '%s\n' "$APPEAR"; }
wait_run_phase() { if [[ $2 == 60 ]]; then printf '%s\n' "$RUNNING"; else printf '%s\n' "$FINAL"; fi; }
` + source[start:end] + "\nprintf 'COUNTS=%s:%s:%s\\n' \"$PASS\" \"$FAIL\" \"$SKIP\"\n"
			out, err := runSmokeFragment(t, dir, script, "APPEAR="+tc.appear, "RUNNING="+tc.running, "FINAL="+tc.final)
			if err != nil {
				t.Fatalf("scenario: %v\n%s", err, out)
			}
			if tc.name == "canceled" {
				if !strings.Contains(out, "COUNTS=3:0:0") {
					t.Fatalf("successful cancellation did not pass:\n%s", out)
				}
			} else if !strings.Contains(out, ":1:0") {
				t.Fatalf("unobserved cancellation did not fail:\n%s", out)
			}
		})
	}
}

func TestSmokeSummaryDistinguishesOptionalSkips(t *testing.T) {
	source := readGateSource(t, "smoke-test.sh")
	start := strings.Index(source, "log_section \"Summary\"")
	if start < 0 {
		t.Fatal("cannot locate smoke summary")
	}
	for _, skipped := range []string{"0", "1"} {
		dir := t.TempDir()
		script := smokePrefix(t) + "\nSKIP=" + skipped + "\n" + source[start:]
		out, err := runSmokeFragment(t, dir, script)
		if err != nil {
			t.Fatalf("summary: %v\n%s", err, out)
		}
		if strings.Contains(out, "All checks passed.") != (skipped == "0") {
			t.Fatalf("summary misrepresented skipped=%s:\n%s", skipped, out)
		}
		if skipped == "1" && !strings.Contains(out, "optional check(s) remain unverified") {
			t.Fatalf("summary did not identify missing optional evidence:\n%s", out)
		}
	}
}

func makeGateFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "charts", "orbitjob"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "charts", "orbitjob", "Chart.yaml"), []byte("appVersion: \"0.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"Makefile", "scripts/run-benchmarks.sh"} {
		source := filepath.Join("..", relative)
		content, err := os.ReadFile(source)
		if os.IsNotExist(err) && relative != "Makefile" {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func gateEnvironment(remove ...string) []string {
	var env []string
	for _, entry := range os.Environ() {
		keep := true
		for _, key := range remove {
			if strings.HasPrefix(entry, key+"=") {
				keep = false
			}
		}
		if keep {
			env = append(env, entry)
		}
	}
	return env
}

func writeGateExecutable(t *testing.T, path, source string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readGateSource(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func smokePrefix(t *testing.T) string {
	t.Helper()
	source := readGateSource(t, "smoke-test.sh")
	end := strings.Index(source, "# ── Bootstrap key")
	if end < 0 {
		t.Fatal("cannot locate smoke helper boundary")
	}
	return source[:end]
}

func runSmokeFragment(t *testing.T, dir, script string, env ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(gateEnvironment("PATH", "APPEAR", "RUNNING", "FINAL"), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

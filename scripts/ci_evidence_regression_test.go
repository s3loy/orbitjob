package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
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

func TestSmokeUsesCheckedDefinitionWrite(t *testing.T) {
	const (
		canonicalSpec = `{"executionPolicyVersion":1,"jobTemplate":{"command":["sleep","30"]},"retryPolicy":{"maxAttempts":1}}`
		digest        = "checked-fixture-digest"
		uid           = "11111111-1111-4111-8111-111111111111"
	)
	type request struct {
		ID     string `json:"request_id"`
		DryRun bool   `json:"dry_run"`
		Spec   string `json:"spec_json"`
		Digest string `json:"manifest_digest"`
	}
	for _, tc := range []struct {
		name         string
		wantRequests int32
		wantResult   string
	}{
		{"accepted", 2, "RESULT=success:1:" + uid},
		{"check rejected", 1, "RESULT=failure:0:"},
		{"wrong check receipt", 1, "RESULT=failure:0:"},
		{"apply unavailable", 2, "RESULT=failure:1:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			var checkedID atomic.Value
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.URL.Path != "/api/v1/definitions/jobs/default/smoke-sleep" || r.Header.Get("Authorization") != "Bearer fixture" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				var in request
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Errorf("invalid request: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				receipt := map[string]string{"request_id": in.ID, "kind": "jobs", "namespace": "default", "name": "smoke-sleep"}
				switch requests.Add(1) {
				case 1:
					if !in.DryRun || in.ID == "" || in.Digest != "" || !strings.Contains(in.Spec, `"executionPolicyVersion": 1`) || !strings.Contains(in.Spec, `["sleep", "30"]`) {
						t.Errorf("unexpected dry run: %+v", in)
					}
					if tc.name == "check rejected" {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					checkedID.Store(in.ID)
					receipt["state"], receipt["spec_json"], receipt["manifest_digest"] = "validated", canonicalSpec, digest
					if tc.name == "wrong check receipt" {
						receipt["name"] = "another-job"
					}
				case 2:
					if in.DryRun || in.ID != checkedID.Load() || in.Spec != canonicalSpec || in.Digest != digest {
						t.Errorf("apply did not replay the checked request: %+v", in)
					}
					if tc.name == "apply unavailable" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					receipt["state"], receipt["uid"] = "accepted", uid
				default:
					t.Errorf("unexpected extra request: %+v", in)
				}
				if err := json.NewEncoder(w).Encode(receipt); err != nil {
					t.Errorf("encode receipt: %v", err)
				}
			}))
			defer server.Close()
			script := smokePrefix(t) + `
API="$FIXTURE_API"
BOOTSTRAP_KEY=fixture
JOB_NAMESPACE=default
if apply_smoke_job smoke-sleep sleep 30; then result=success; else result=failure; fi
printf 'RESULT=%s:%s:%s\n' "$result" "${#CRS_TO_DELETE[@]}" "${JOB_UID:-}"
`
			out, err := runSmokeFragment(t, t.TempDir(), script, "FIXTURE_API="+server.URL)
			if err != nil || !strings.Contains(out, tc.wantResult) || requests.Load() != tc.wantRequests {
				t.Fatalf("wanted %s after %d requests; got %d, err=%v:\n%s", tc.wantResult, tc.wantRequests, requests.Load(), err, out)
			}
		})
	}
}

func TestSmokeCleanupRequiresMatchingFixture(t *testing.T) {
	const (
		uid      = "11111111-1111-4111-8111-111111111111"
		observed = `{"metadata":{"uid":"` + uid + `","resourceVersion":"42","annotations":{"orbitjob.io/admin-request-id":"fixture-request"}}}`
	)
	source := readGateSource(t, "smoke-test.sh")
	start := strings.Index(source, "cleanup_crs() {")
	end := strings.Index(source, "trap cleanup_crs EXIT")
	if start < 0 || end <= start {
		t.Fatal("cannot locate smoke cleanup")
	}
	for _, tc := range []struct {
		name, acceptedUID, observed, wantDiagnostic string
		deleteConflicts, wantDeleteRequest          bool
	}{
		{"accepted fixture", uid, observed, "", false, true},
		{"accepted fixture missing from this context", uid, "", "could not match ScheduledJob", false, false},
		{"recreated fixture", uid, strings.Replace(observed, uid, "wrong-uid", 1), "could not match ScheduledJob", false, false},
		{"ambiguous write with copied annotation", "", observed, "operation fixture-request has no confirmed UID", false, false},
		{"ambiguous write did not persist", "", "", "", false, false},
		{"delete precondition conflict", uid, observed, "could not confirm the guarded delete", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			shim := filepath.Join(dir, "fakebin")
			writeGateExecutable(t, filepath.Join(shim, "kubectl"), `#!/usr/bin/env bash
set -eu
[[ $1 == --context && $2 == fixture-context && $3 == -n && $4 == default && $5 == --request-timeout=30s ]]
case $6 in
  get) printf '%s' "$FIXTURE_LIVE" ;;
  delete)
    [[ $7 == --raw && $8 == /apis/workloads.orbitjob.io/v1alpha1/namespaces/default/scheduledjobs/smoke-sleep && $9 == -f && ${10} == - ]]
    cat > delete-request
    if [[ $FIXTURE_DELETE_CONFLICTS == true ]]; then
      echo 'Error from server (Conflict): resourceVersion precondition failed' >&2
      exit 1
    fi ;;
  wait) test -s delete-request && touch wait-called ;;
  *) exit 1 ;;
esac
`)
			script := smokePrefix(t) + source[start:end] + `
KUBE_CONTEXT=fixture-context
JOB_NAMESPACE=default
CRS_TO_DELETE=("smoke-sleep:fixture-request:$FIXTURE_UID")
cleanup_crs
`
			out, err := runSmokeFragment(t, dir, script, "PATH="+shim+":"+os.Getenv("PATH"), "FIXTURE_UID="+tc.acceptedUID, "FIXTURE_LIVE="+tc.observed,
				"FIXTURE_DELETE_CONFLICTS="+strconv.FormatBool(tc.deleteConflicts))
			if (err == nil) != (tc.wantDiagnostic == "") || !strings.Contains(out, tc.wantDiagnostic) {
				t.Fatalf("wanted diagnostic %q, err=%v:\n%s", tc.wantDiagnostic, err, out)
			}
			_, waitErr := os.Stat(filepath.Join(dir, "wait-called"))
			if shouldWait := tc.wantDeleteRequest && !tc.deleteConflicts; (waitErr == nil) != shouldWait {
				t.Fatalf("wait for deletion called=%v, want %v", waitErr == nil, shouldWait)
			}
			body, readErr := os.ReadFile(filepath.Join(dir, "delete-request"))
			if !tc.wantDeleteRequest {
				if !os.IsNotExist(readErr) {
					t.Fatalf("cleanup attempted an unrelated delete: %q, err=%v", body, readErr)
				}
				return
			}
			var opts struct {
				Preconditions struct {
					UID             string `json:"uid"`
					ResourceVersion string `json:"resourceVersion"`
				} `json:"preconditions"`
			}
			if readErr != nil {
				t.Fatal(readErr)
			}
			if err := json.Unmarshal(body, &opts); err != nil || opts.Preconditions.UID != uid || opts.Preconditions.ResourceVersion != "42" {
				t.Fatalf("cleanup must delete the observed UID and resourceVersion, got %s, err=%v", body, err)
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

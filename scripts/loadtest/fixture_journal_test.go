package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Execute the shipped fixture source on loopback. The only mutant disables
// the dedupe branch; the journal and its HTTP read path remain unchanged.
func startJournalFixture(t *testing.T, disableDedupe bool) string {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("python3 is required to verify the real fixture evidence source")
	}
	data, err := os.ReadFile("../../deploy/load/fixture-configmap.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	source := config.Data["server.py"]
	if disableDedupe {
		if strings.Count(source, "if not duplicate:") != 1 {
			t.Fatal("cannot identify the dedupe negative control")
		}
		source = strings.Replace(source, "if not duplicate:", "if True:  # negative control: dedupe disabled", 1)
	}
	path := filepath.Join(t.TempDir(), "fixture.py")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	launcher := "import runpy,sys; from http.server import ThreadingHTTPServer; scope=runpy.run_path(sys.argv[1]); server=ThreadingHTTPServer(('127.0.0.1',0),scope['Handler']); print(server.server_port,flush=True); server.serve_forever()"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	cmd := exec.CommandContext(ctx, python, "-u", "-c", launcher, path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatalf("fixture never became ready: %v %s", scanner.Err(), stderr.String())
	}
	return "http://127.0.0.1:" + strings.TrimSpace(scanner.Text())
}

func TestRealFixtureJournalCanExposeDuplicateSideEffects(t *testing.T) {
	for _, mutant := range []bool{false, true} {
		t.Run(fmt.Sprintf("dedupe_disabled=%v", mutant), func(t *testing.T) {
			base := startJournalFixture(t, mutant)
			client := &http.Client{Timeout: time.Second}
			for i := 0; i < 2; i++ {
				response, err := client.Post(base+"/ledger", "application/json", strings.NewReader(`{"case_id":"this-run-case","operation":"delivery","idempotency_key":"this-run-delivery"}`))
				if err != nil {
					t.Fatal(err)
				}
				_ = response.Body.Close()
			}
			entries, err := collectFixtureJournal(context.Background(), base, map[string]bool{"this-run-case": true})
			if err != nil {
				t.Fatal(err)
			}
			check := checkDuplicateLedger(entries)
			want, count := VerdictPass, 1
			if mutant {
				want, count = VerdictFail, 2
			}
			if check.Status != want || len(entries) != count {
				t.Fatalf("real source rows=%d verdict=%s, want %d/%s", len(entries), check.Status, count, want)
			}
		})
	}
}

func TestFixtureJournalRejectsEmptyForeignAndErrorSources(t *testing.T) {
	for _, body := range []string{`[]`, `[{"case_id":"foreign-run","idempotency_key":"key","operation":"delivery"}]`, `[{"case_id":"this-run-case"}]`, `null`, `not-json`} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer srv.Close()
			if _, err := collectFixtureJournal(context.Background(), srv.URL, map[string]bool{"this-run-case": true}); err == nil {
				t.Fatal("missing/malformed evidence source accepted")
			}
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`[{"case_id":"this-run-case","idempotency_key":"key","operation":"delivery"}]`))
	}))
	defer srv.Close()
	if _, err := collectFixtureJournal(context.Background(), srv.URL, map[string]bool{"this-run-case": true}); err == nil {
		t.Fatal("HTTP500 journal accepted")
	}
}

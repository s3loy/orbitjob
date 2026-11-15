package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHostedSettleDoesNotAcceptUnscopedOrFailedCounts(t *testing.T) {
	source, err := os.ReadFile("../loadtest-ci.sh")
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(source), "log \"waiting for runs to reach a terminal state\"\n")
	if !ok {
		t.Fatal("settle block missing")
	}
	block, _, ok = strings.Cut(block, "log \"verifying\"")
	if !ok {
		t.Fatal("verification boundary missing")
	}
	for _, tc := range []struct {
		name, response, attempts string
		settled                  bool
	}{
		{"running then terminal", "case $attempt in 1) echo 3;; *) echo 0;; esac", "2", true},
		{"failed zero then running", "case $attempt in 1) echo 0; exit 1;; 2) echo 2;; *) echo 0;; esac", "3", true},
		{"malformed count", "echo malformed", "60", false},
		{"failed zero until timeout", "echo 0; exit 1", "60", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			write("go", `
test "$*" = 'run ./scripts/loadtest settle --run-id regression-run' || exit 7
attempt=0
if test -f "$SETTLE_COUNTER"; then attempt=$(cat "$SETTLE_COUNTER"); fi
attempt=$((attempt+1))
echo "$attempt" > "$SETTLE_COUNTER"
`+tc.response)
			// Reproduce the old RLS-masked zero if the script bypasses settle.
			write("psql", "echo 0")
			write("sleep", "exit 0")
			// Sixty mock polls fork several shell processes each. This bounds a
			// hung fixture, not a product latency claim; retain exact poll-count
			// and verdict assertions when CI is also compiling other packages.
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", "-c", `
set -euo pipefail
log() { printf '%s\n' "$*" >&2; }
run_id=regression-run
admin_dsn=credential-canary
`+block+`
printf 'settled=%s\n' "$settled"
`)
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "SETTLE_COUNTER="+filepath.Join(dir, "count"))
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("settle shell: %v: %s", err, output)
			}
			count, err := os.ReadFile(filepath.Join(dir, "count"))
			if err != nil || strings.TrimSpace(string(count)) != tc.attempts {
				t.Fatalf("scoped poll attempts=%q, want %s (read error %v); output=%s", count, tc.attempts, err, output)
			}
			want := "settled=false"
			if tc.settled {
				want = "settled=true"
			}
			if !strings.Contains(string(output), want) || strings.Contains(string(output), "credential-canary") {
				t.Fatalf("unsafe settle output: %s", output)
			}
			if !tc.settled && !strings.Contains(string(output), "WARN") {
				t.Fatalf("timeout did not warn: %s", output)
			}
		})
	}
}

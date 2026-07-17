package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReportIncludesRequiredSections(t *testing.T) {
	dir := t.TempDir()
	run := RunRecord{RunID: "test", Qualification: true, Commit: "abc", Environment: Environment{DockerCPU: 10, DockerMemoryBytes: 8 << 30}}
	result := Result{Verdict: VerdictPass, Reason: "all gates passed", Checks: []CheckResult{{ID: "tenant-leak", Status: VerdictPass}}}
	if err := WriteReport(dir, run, result, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"# v0.2.0 Load Qualification Report", "## Verdict", "## Environment", "## Correctness", "## Evidence"} {
		if !bytes.Contains(body, []byte(section)) {
			t.Fatalf("missing section %s", section)
		}
	}
}

func TestNonStandardRunHasWarning(t *testing.T) {
	dir := t.TempDir()
	run := RunRecord{RunID: "smoke", Qualification: false}
	result := Result{Verdict: VerdictPass, Reason: "non-standard run completed"}
	if err := WriteReport(dir, run, result, nil); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "report.md"))
	if !bytes.Contains(body, []byte("NON-STANDARD RUN")) {
		t.Fatal("non-standard run missing warning")
	}
}

func TestCleanupRequiresExactRunID(t *testing.T) {
	if err := ValidateCleanRunID(""); err == nil {
		t.Fatal("empty run ID rejected")
	}
	if err := ValidateCleanRunID("*"); err == nil {
		t.Fatal("wildcard rejected")
	}
	if got := CleanupSelector("run-123"); got != "orbitjob.io/load-run=run-123" {
		t.Fatalf("selector = %q", got)
	}
}

func TestWriteReportIncludesRunStats(t *testing.T) {
	dir := t.TempDir()
	run := RunRecord{RunID: "test", Qualification: true, Commit: "abc"}
	result := Result{Verdict: VerdictPass, Reason: "all gates passed"}
	stats := &RunStats{
		ScheduledEvents: 100, Triggered: 100, Accepted: 98, Rejected: 2, Skipped: 0,
		Breakdown: RejectionBreakdown{RateLimited: 2},
		Completed: true,
	}
	if err := WriteReport(dir, run, result, stats); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "report.md"))
	for _, want := range []string{"## Run Stats", "Triggered: 100", "rate_limited=2"} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("report missing %q:\n%s", want, body)
		}
	}
}

func TestWriteChecksumsComputesRealSHA256(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteChecksums(dir); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	// sha256("hello") = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	if !bytes.Contains(body, []byte("2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824  a.txt")) {
		t.Fatalf("checksums not real sha256: %s", body)
	}
}

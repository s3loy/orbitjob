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
	if err := WriteReport(dir, run, result); err != nil {
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
	if err := WriteReport(dir, run, result); err != nil {
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

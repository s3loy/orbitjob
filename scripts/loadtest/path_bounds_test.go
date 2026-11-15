package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReportRejectsTraversalBeforeWriting(t *testing.T) {
	parent := t.TempDir()
	runRoot := filepath.Join(parent, "runs")
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "result.json"), []byte(`{"verdict":"PASS"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runReport([]string{"--run-id", "../outside", "--run-root", runRoot})
	if err == nil || !strings.Contains(err.Error(), "invalid run ID") {
		t.Fatalf("report accepted traversal or reached I/O first: %v", err)
	}
	for _, file := range []string{"report.md", "checksums.txt"} {
		if _, err := os.Stat(filepath.Join(outside, file)); !os.IsNotExist(err) {
			t.Errorf("report wrote outside its root: %s", file)
		}
	}
}

func TestPrepareRejectsTraversalBeforeLoadingConfig(t *testing.T) {
	parent := t.TempDir()
	err := Prepare("missing-config", "missing-images", "../outside", filepath.Join(parent, "runs"), "", "", "smoke")
	if err == nil || !strings.Contains(err.Error(), "invalid run ID") {
		t.Fatalf("prepare reached I/O before validating run ID: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "outside")); !os.IsNotExist(err) {
		t.Error("prepare created a directory outside its root")
	}
}

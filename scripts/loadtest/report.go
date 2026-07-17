package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func WriteReport(runDir string, run RunRecord, result Result, stats *RunStats) error {
	body := buildReportBody(run, result, stats)
	if err := os.WriteFile(filepath.Join(runDir, "result.json"), mustJSON(result), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runDir, "report.md"), []byte(body), 0o600)
}

func buildReportBody(run RunRecord, result Result, stats *RunStats) string {
	var b strings.Builder
	title := "v0.2.0 Load Qualification Report"
	if !run.Qualification {
		b.WriteString("NON-STANDARD RUN - NOT A RELEASE QUALIFICATION\n\n")
	}
	fmt.Fprintf(&b, "# %s\n\n## Verdict\n\n%s: %s\n\n", title, result.Verdict, result.Reason)
	fmt.Fprintf(&b, "## Environment\n\n- Commit: %s (dirty=%v)\n- Docker: %d CPU / %.2f GiB\n\n", run.Commit, run.Dirty, run.Environment.DockerCPU, float64(run.Environment.DockerMemoryBytes)/(1<<30))
	if stats != nil {
		b.WriteString("## Run Stats\n\n")
		fmt.Fprintf(&b, "- Window: %s → %s (completed=%v)\n", stats.StartedAt.Format(time.RFC3339), stats.FinishedAt.Format(time.RFC3339), stats.Completed)
		fmt.Fprintf(&b, "- Scheduled events: %d\n- Triggered: %d\n- Accepted: %d\n- Rejected: %d (rate_limited=%d server=%d transport=%d other=%d)\n- Skipped: %d\n\n",
			stats.ScheduledEvents, stats.Triggered, stats.Accepted,
			stats.Rejected, stats.Breakdown.RateLimited, stats.Breakdown.Server, stats.Breakdown.Transport, stats.Breakdown.Other,
			stats.Skipped)
	}
	b.WriteString("## Correctness\n\n")
	for _, check := range result.Checks {
		fmt.Fprintf(&b, "- %s: %s\n", check.ID, check.Status)
	}
	b.WriteString("\n## Evidence\n\n")
	b.WriteString("See checksums.txt and samples/ for detailed evidence.\n")
	return b.String()
}

func WriteChecksums(runDir string) error {
	var lines []string
	err := filepath.Walk(runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Base(path) == "checksums.txt" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read for checksum %s: %w", path, err)
		}
		sum := sha256.Sum256(data)
		rel, _ := filepath.Rel(runDir, path)
		lines = append(lines, fmt.Sprintf("%x  %s", sum, rel))
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(lines)
	return os.WriteFile(filepath.Join(runDir, "checksums.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func gitCommit() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitDirty() bool {
	out, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

func CleanupSelector(runID string) string {
	return "orbitjob.io/load-run=" + runID
}

func ValidateCleanRunID(runID string) error {
	if runID == "" || strings.ContainsAny(runID, "*?") {
		return fmt.Errorf("invalid run ID %q", runID)
	}
	return nil
}

func mustJSON(value any) []byte {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(data, '\n')
}

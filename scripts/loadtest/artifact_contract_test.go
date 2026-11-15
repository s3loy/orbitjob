package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orbitjob/internal/platform/scan"
)

func TestReportRefusesMissingCorruptOrMixedEvidence(t *testing.T) {
	for _, change := range []string{"none", "missing record", "corrupt record", "missing stats", "corrupt stats", "foreign result", "foreign stats", "qualification", "source", "invalid verdict", "incomplete", "legacy config", "missing checks", "inconclusive check", "wrong counter", "missing journal", "foreign journal", "missing fault check", "self-report fault"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "current")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			run := RunRecord{RunID: "current", Profile: "smoke", Seed: "seed", Commit: "execution-source", StartedAt: time.Now()}
			stats := RunStats{RunID: run.RunID, Profile: run.Profile, Seed: run.Seed, Commit: run.Commit, StartedAt: run.StartedAt}
			stats.Completed, stats.ConfigSHA256 = true, configSHA256(Config{})
			stats.JournalStart = &JournalCursor{RunID: run.RunID, BootID: "fixture-epoch"}
			stats.ScheduledEvents, stats.Triggered, stats.Accepted = 1, 1, 1
			stats.AcceptedTriggers = []AcceptedTrigger{{Tenant: scan.GenerateID(), RevisionID: 7, OccurrenceKey: "observed"}}
			e := completeVerificationEvidence()
			e.ManualRuns = stats.AcceptedTriggers
			e.ExecutionChecks = executionEvidenceChecks(stats, e)
			e.ExecutionChecks = append(e.ExecutionChecks, CheckResult{ID: "journal-continuity", Status: VerdictPass})
			result := EvaluateQualification(run, e, 0)
			switch change {
			case "foreign result":
				result.RunID = "older"
			case "foreign stats":
				stats.RunID = "older"
			case "qualification":
				result.Qualification = true
			case "source":
				stats.Commit = "other-source"
			case "invalid verdict":
				result.Verdict = "GREEN"
			case "incomplete":
				stats.Completed = false
			case "legacy config":
				stats.ConfigSHA256 = ""
			case "missing checks":
				result.Checks = nil
			case "inconclusive check":
				result.Checks[0].Status = VerdictInconclusive
			case "wrong counter":
				stats.Accepted = 2
			case "missing journal":
				stats.JournalStart = nil
			case "foreign journal":
				stats.JournalStart.RunID = "another-run"
			case "missing fault check":
				stats.FaultPlan = []FaultSpec{{Name: "operator", Offset: time.Second}}
			case "self-report fault":
				stats.FaultPlan = []FaultSpec{{Name: "operator", Offset: time.Second}}
				stats.Faults = []FaultRecord{{Name: "operator", ScheduledOffset: time.Second, InjectionSucceeded: true, Ready: true, BusinessRecovered: true, BusinessEvidence: "claimed recovered"}}
				result.Checks = append(result.Checks, CheckResult{ID: faultCheckID(stats.FaultPlan[0]), Status: VerdictPass})
			}
			for name, value := range map[string]any{"run-record.json": run, "run-stats.json": stats, "result.json": result} {
				if err := writeStableJSON(filepath.Join(dir, name), value); err != nil {
					t.Fatal(err)
				}
			}
			for _, artifact := range []struct{ label, path string }{{"record", "run-record.json"}, {"stats", "run-stats.json"}} {
				if change == "missing "+artifact.label {
					if err := os.Remove(filepath.Join(dir, artifact.path)); err != nil {
						t.Fatal(err)
					}
				}
				if change == "corrupt "+artifact.label {
					if err := os.WriteFile(filepath.Join(dir, artifact.path), []byte("{"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			err := runReport([]string{"--run-root", root, "--run-id", "current"})
			if (err == nil) != (change == "none") {
				t.Fatalf("report error=%v", err)
			}
			if change != "none" {
				if _, err := os.Stat(filepath.Join(dir, "report.md")); !os.IsNotExist(err) {
					t.Fatalf("published rejected report: %v", err)
				}
			}
		})
	}
}

func TestVerifyRejectsConfigDriftBeforeReadingExternalEvidence(t *testing.T) {
	root := t.TempDir()
	_, defs := generateIdentityFixture(t, root, "recorded")
	cfg, err := LoadConfig("../../test/load/config/smoke.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = resolveRunTenants(cfg, root, "recorded", false)
	if err != nil {
		t.Fatal(err)
	}
	created := make([]CreatedDefinition, 0, len(defs))
	keys := map[string]string{}
	namespaces := map[string]string{}
	for i, def := range defs {
		created = append(created, CreatedDefinition{CaseID: def.CaseID, Tenant: def.Tenant, RevisionID: int64(i + 1), ExpectedTerminalState: def.Expected.TerminalState})
		keys[def.Tenant] = "private-key"
		namespaces[def.Tenant] = TenantNamespace(def.Tenant)
	}
	stats := RunStats{RunID: "recorded", Profile: cfg.Profile, Seed: cfg.Seed, ConfigSHA256: configSHA256(cfg)}
	dir := filepath.Join(root, "recorded")
	for name, value := range map[string]any{"created-definitions.json": created, "tenant-keys.json": keys, "tenant-namespaces.json": namespaces, "run-stats.json": stats} {
		if err := writeStableJSON(filepath.Join(dir, name), value); err != nil {
			t.Fatal(err)
		}
	}
	// The profile name and seed still agree. A changed minimum used to alter
	// qualification without any evidence that this was the executed workload.
	text, err := os.ReadFile("../../test/load/config/smoke.yaml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "changed.yaml")
	text = []byte(strings.Replace(string(text), "minimum_instances: 0", "minimum_instances: 1", 1))
	if err := os.WriteFile(path, text, 0600); err != nil {
		t.Fatal(err)
	}
	err = runVerify([]string{"--run-root", root, "--run-id", "recorded", "--config", path, "--orbitjob-dsn", "bad-private-dsn", "--api-url", "http://127.0.0.1:1"})
	if err == nil || !strings.Contains(err.Error(), "execution config") || strings.Contains(err.Error(), "bad-private-dsn") {
		t.Fatalf("changed config reached external collection or passed: %v", err)
	}
}

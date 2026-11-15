package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orbitjob/internal/platform/scan"
)

func TestGenerationCannotReplacePreparedCorpusUnderTheSameRunID(t *testing.T) {
	root := t.TempDir()
	_, definitions := generateIdentityFixture(t, root, "recorded")
	dir := filepath.Join(root, "recorded")
	if err := writeStableJSON(filepath.Join(dir, "created-definitions.json"), []CreatedDefinition{{CaseID: definitions[0].CaseID, RevisionID: 7, Tenant: definitions[0].Tenant}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "generated", "definitions.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.ReadFile("../../test/load/config/images.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// A valid lock with a different pinned digest changes the workload body
	// without changing config, seed, case names, tenants or terminal states.
	lockPath := filepath.Join(root, "images.yaml")
	// Keep the digest grammar/length valid by rotating its first hex digit.
	changed := string(lock)
	index := strings.Index(changed, "sha256:") + len("sha256:")
	replacement := "0"
	if changed[index] == '0' {
		replacement = "1"
	}
	changed = changed[:index] + replacement + changed[index+1:]
	if err := os.WriteFile(lockPath, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	err = runGenerate([]string{"--config", "../../test/load/config/smoke.yaml", "--profile", "smoke", "--images", lockPath, "--scenarios", "../../test/load/scenarios", "--run-root", root, "--run-id", "recorded"})
	if err == nil || !strings.Contains(err.Error(), "generated corpus differs") {
		t.Fatalf("prepared corpus replacement accepted or unrelated failure: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rejected generation changed the saved corpus")
	}
}

func TestGenerationConfigCannotDriftBeforePrepareOrExecution(t *testing.T) {
	root := t.TempDir()
	generateIdentityFixture(t, root, "recorded")
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"minimum", func(c *Config) { c.MinimumInstances++ }},
		{"duration", func(c *Config) { c.Duration += time.Minute; c.DurationText = c.Duration.String() }},
		{"rate", func(c *Config) { c.Phases[0].RatePerMinute++ }},
		{"cron", func(c *Config) { c.Definitions.CronIntervalMinutes++ }},
		{"corpus", func(c *Config) { c.Definitions.Total++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadConfig("../../test/load/config/smoke.yaml")
			if err != nil {
				t.Fatal(err)
			}
			tc.change(&cfg)
			for _, create := range []bool{false, true} {
				if _, err := resolveRunTenants(cfg, root, "recorded", create); err == nil {
					t.Fatal("changed config accepted for the old generated corpus")
				}
			}
		})
	}
}

func TestExplicitRunTenantsCannotBeSilentlyReplaced(t *testing.T) {
	root := t.TempDir()
	cfg, err := LoadConfig("../../test/load/config/smoke.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantCount = 0
	if _, err := resolveRunTenants(cfg, root, "explicit", true); err != nil {
		t.Fatal(err)
	}
	cfg.Tenants[0] = scan.GenerateID()
	if _, err := resolveRunTenants(cfg, root, "explicit", false); err == nil {
		t.Fatal("changed explicit tenant was silently replaced")
	}
}

package migrations

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestV020MigrationFileSet(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	want := []string{"0001_v020_baseline.up.sql"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("migration files = %v, want %v", files, want)
	}
}

func TestV020BaselineContainsRequiredPhases(t *testing.T) {
	data, err := os.ReadFile("0001_v020_baseline.up.sql")
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	sqlText := string(data)
	for _, fragment := range []string{
		"CREATE EXTENSION IF NOT EXISTS pgcrypto",
		"required database role",
		"ALTER TABLE jobs FORCE ROW LEVEL SECURITY",
		"ALTER TABLE api_keys FORCE ROW LEVEL SECURITY",
		"SECURITY DEFINER",
		"SET search_path = pg_catalog, public",
		"REVOKE ALL ON FUNCTION public.orbitjob_auth_api_key(text) FROM PUBLIC",
		"orbitjob_bootstrap_default",
		"v0.2.0 catalog assertion failed",
	} {
		if !strings.Contains(sqlText, fragment) {
			t.Errorf("baseline missing %q", fragment)
		}
	}
}

func TestV020BaselineDoesNotCreateClusterRoles(t *testing.T) {
	data, err := os.ReadFile("0001_v020_baseline.up.sql")
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	if strings.Contains(string(data), "CREATE ROLE ") {
		t.Fatal("baseline must not create cluster-level roles")
	}
}

func TestV020BaselineForcesTenantRLS(t *testing.T) {
	data, err := os.ReadFile("0001_v020_baseline.up.sql")
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	sqlText := string(data)
	for _, table := range []string{
		"tenants", "jobs", "job_instances", "job_instance_attempts", "workers",
		"audit_events", "job_change_audits", "checks", "check_runs", "slis",
		"slos", "sli_snapshots", "budgets", "budget_alerts", "api_keys",
	} {
		for _, action := range []string{
			"ALTER TABLE " + table + " ENABLE ROW LEVEL SECURITY",
			"ALTER TABLE " + table + " FORCE ROW LEVEL SECURITY",
		} {
			if !strings.Contains(sqlText, action) {
				t.Errorf("baseline missing %q", action)
			}
		}
	}
}

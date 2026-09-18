package migrations

import (
	"bytes"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// These tests need no database. They cover the properties the catalog cannot
// express: which files exist, and the absence of statements whose effect would
// only be visible later (a role created at cluster scope, a default privilege
// that grants writes on tables a future migration has not written yet).

func TestMigrationFileSet(t *testing.T) {
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
	// 0001-0005 collapsed into a single baseline on 2026-09-17 (PLAN.md D2).
	// 0002 puts checks on the Kubernetes control plane: the scheduled_for
	// ledger column, the slis.source_type cutover to job_run, and the
	// sli_snapshots truncate (checks-to-k8s-jobs design, rulings R5-R7).
	// 0003 adds the resource_group_id scoping column to the ledger tables.
	// 0004 adds the Functions surface: the functions configuration table and
	// the function_runs derived read model (serverless design, section 4).
	// 0005 adds the Workflows surface: workflow_run_control_plane and the
	// workflow_run_id step-grouping column (workflow design, section 9.1).
	// 0006 arms the enforcing function/workflow routes: the TenantAdminAccess
	// preset document gains the six actions the routes check (the 0004/0005
	// preset deferral, kept because an action earns a preset place only when
	// the HTTP API enforces it).
	want := []string{
		"0001_baseline.up.sql",
		"0002_checks_to_kubernetes_jobs.up.sql",
		"0003_resource_group_id_on_ledger.up.sql",
		"0004_functions.up.sql",
		"0005_workflows.up.sql",
		"0006_tenant_admin_function_workflow_actions.up.sql",
	}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("migration files = %v, want %v", files, want)
	}
}

// TestBaselineDoesNotCreateClusterRoles is the reason the baseline can run under
// the restricted migrator identity at all. A CREATE ROLE needs CREATEROLE, which
// no deployment role holds; owner-init creates the roles and the baseline only
// checks that they exist.
func TestBaselineDoesNotCreateClusterRoles(t *testing.T) {
	data := readBaseline(t)
	if strings.Contains(string(data), "CREATE ROLE ") {
		t.Fatal("baseline must not create cluster-level roles")
	}
}

// TestBaselineHasNoDefaultPrivilegesForAdmin guards the run-ledger rule from the
// one direction a grant check cannot see. A present-day GRANT is checked when
// the schema is applied; an ALTER DEFAULT PRIVILEGES is not, because it writes
// grants on tables that do not exist yet. The rule the ledger depends on is that
// orbitjob_admin gets write access only when a migration names the table, so a
// blanket default privilege to admin must never appear.
func TestBaselineHasNoDefaultPrivilegesForAdmin(t *testing.T) {
	for i, line := range strings.Split(string(readBaseline(t)), "\n") {
		code := line
		if idx := strings.Index(code, "--"); idx >= 0 {
			code = code[:idx]
		}
		if !strings.Contains(strings.ToUpper(code), "ALTER DEFAULT PRIVILEGES") {
			continue
		}
		if strings.Contains(code, "orbitjob_admin") {
			t.Fatalf("line %d grants default privileges to orbitjob_admin: %s", i+1, strings.TrimSpace(line))
		}
	}
}

// The Helm chart ships its own copy of the up migrations so the migrate Job can
// run them. A copy that drifts from db/migrations lets `helm upgrade` apply a
// schema the code was not built against, and the drift is silent because both
// files still exist and still have the same name. The file set is not enough;
// the bytes have to match.
func TestChartMigrationsMatchSourceOfTruth(t *testing.T) {
	sourceFiles, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read source migrations: %v", err)
	}
	const chartDir = "../../charts/orbitjob/migrations"
	chartFiles, err := os.ReadDir(chartDir)
	if err != nil {
		t.Fatalf("read chart migrations: %v", err)
	}

	chart := make(map[string]bool, len(chartFiles))
	for _, entry := range chartFiles {
		chart[entry.Name()] = true
	}

	var upMigrations []string
	for _, entry := range sourceFiles {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		upMigrations = append(upMigrations, name)
		if !chart[name] {
			t.Errorf("chart is missing up migration %s", name)
		}
	}

	source := make(map[string]bool, len(sourceFiles))
	for _, entry := range sourceFiles {
		source[entry.Name()] = true
	}
	for name := range chart {
		if !source[name] {
			t.Errorf("chart has migration %s absent from db/migrations", name)
		}
	}

	for _, name := range upMigrations {
		want, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		got, err := os.ReadFile(chartDir + "/" + name)
		if err != nil {
			t.Fatalf("read chart %s: %v", name, err)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("chart copy of %s differs from db/migrations; run `scripts/helm-migrations.sh sync`", name)
		}
	}
}

func readBaseline(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("0001_baseline.up.sql")
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	return data
}

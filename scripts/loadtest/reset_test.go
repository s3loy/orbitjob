package main

import (
	"strings"
	"testing"
)

// The reset's database steps were once a tenant-blind TRUNCATE of the whole
// control-plane ledger, which wiped the default tenant's check history and
// orphaned its JobRun CRs (2026-09-19). These pins keep the wipe scoped to
// the load-test tenants and keep the FK-safe ordering honest.
func TestResetDeletesAreScopedToLoadTenants(t *testing.T) {
	for _, step := range resetSteps() {
		statement := step.cmd[len(step.cmd)-1]
		if strings.Contains(strings.ToUpper(statement), "TRUNCATE") {
			t.Errorf("step %q truncates instead of deleting: %s", step.name, statement)
		}
		if !strings.Contains(statement, "DELETE FROM") {
			continue
		}
		if !strings.Contains(statement, defaultTenantGuard) {
			t.Errorf("step %q deletes without the default-tenant guard: %s", step.name, statement)
		}
	}
}

func TestResetCoversEveryRestrictLedgerTable(t *testing.T) {
	// function_runs and workflow_run_control_plane hold ON DELETE RESTRICT
	// tenant foreign keys since migrations 0004/0005; leaving them out makes
	// the tenant delete reject and the reset fail halfway.
	combined := ""
	for _, step := range resetSteps() {
		combined += step.cmd[len(step.cmd)-1] + "\n"
	}
	for _, table := range []string{
		"job_run_attempts_control_plane",
		"job_run_control_plane",
		"workflow_run_control_plane",
		"function_runs",
		"job_definition_revisions",
	} {
		if !strings.Contains(combined, "DELETE FROM "+table) {
			t.Errorf("reset never deletes load-tenant rows from %s", table)
		}
	}
}

func TestResetLedgerOrderHonorsTheRestrictChain(t *testing.T) {
	// Within the combined ledger statement the deletes must run attempts
	// before runs, runs before workflow runs and revisions: the RESTRICT
	// chain forbids the reverse.
	var ledger string
	for _, step := range resetSteps() {
		if step.name == "delete load-tenant control-plane ledger rows" {
			ledger = step.cmd[len(step.cmd)-1]
		}
	}
	if ledger == "" {
		t.Fatal("no ledger delete step")
	}
	order := []string{
		"DELETE FROM job_run_attempts_control_plane",
		"DELETE FROM job_run_control_plane",
		"DELETE FROM workflow_run_control_plane",
		"DELETE FROM function_runs",
		"DELETE FROM job_definition_revisions",
	}
	last := -1
	for _, prefix := range order {
		at := strings.Index(ledger, prefix)
		if at < 0 {
			t.Fatalf("ledger statement is missing %q", prefix)
		}
		if at < last {
			t.Fatalf("%q runs after a table it must precede: %s", prefix, ledger)
		}
		last = at
	}
}

func TestResetPsqlStepsStopOnError(t *testing.T) {
	// psql without ON_ERROR_STOP exits zero after a failed statement, so a
	// half-wiped ledger would be reported as [OK].
	for _, step := range resetSteps() {
		if step.cmd[0] != "kubectl" {
			continue
		}
		on := false
		for _, arg := range step.cmd {
			if arg == "ON_ERROR_STOP=1" {
				on = true
			}
		}
		if !on {
			t.Errorf("step %q runs psql without ON_ERROR_STOP", step.name)
		}
	}
}

func TestResetTenantsGoLast(t *testing.T) {
	steps := resetSteps()
	if got := steps[len(steps)-1].name; got != "delete load-test tenants" {
		t.Fatalf("last database step is %q; the tenant delete must go after every RESTRICT table is cleared", got)
	}
}

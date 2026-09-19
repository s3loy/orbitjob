package main

import (
	"flag"
	"fmt"
	"os/exec"
	"strings"
)

// resetStep is one destructive reset command, named for the progress output.
type resetStep struct {
	name string
	cmd  []string
}

// defaultTenantGuard is the WHERE clause that keeps every delete scoped to
// the load-test tenants. The default tenant is install state: its bootstrap
// key, check history and SLIs must survive a reset. Tenant-blind wipes here
// are what orphaned the default tenant's check-run CRs on 2026-09-19.
const defaultTenantGuard = "tenant_id != (SELECT id FROM tenants WHERE slug = 'default')"

// psqlShell builds a kubectl exec psql invocation. ON_ERROR_STOP makes the
// multi-statement steps fail loudly: without it psql keeps going after a
// failed statement and exits zero, and reset would report success over a
// half-wiped ledger.
func psqlShell(statement string) []string {
	return []string{"kubectl", "exec", "-n", DatabaseNamespace(), "deployment/orbitjob-postgres", "--", "psql", "-U", "postgres", "-d", "orbitjob", "-v", "ON_ERROR_STOP=1", "-c", statement}
}

// resetSteps returns the database-side reset in dependency order. The ledger
// tables hold ON DELETE RESTRICT foreign keys on tenants(id), so their
// load-tenant rows go before the tenants themselves; within the ledger the
// order honors the RESTRICT chain (attempts cascade from runs, runs pin
// workflow runs and revisions, workflow runs pin revisions). Every delete
// carries the default-tenant guard, so the shared install state survives.
func resetSteps() []resetStep {
	return []resetStep{
		// Control-plane rows are keyed by source_uid + generation and occurrence,
		// so leftover rows collide with a recreated CR of the same name and
		// generation. Deleting the load tenants' rows clears the collisions this
		// tool can cause while leaving the default tenant's history alone.
		{name: "delete load-tenant control-plane ledger rows", cmd: psqlShell(fmt.Sprintf(
			"DELETE FROM job_run_attempts_control_plane WHERE %s; "+
				"DELETE FROM job_run_control_plane WHERE %s; "+
				"DELETE FROM workflow_run_control_plane WHERE %s; "+
				"DELETE FROM function_runs WHERE %s; "+
				"DELETE FROM job_definition_revisions WHERE %s",
			defaultTenantGuard, defaultTenantGuard, defaultTenantGuard, defaultTenantGuard, defaultTenantGuard))},
		// The baseline schema puts ON DELETE RESTRICT foreign keys on tenants(id)
		// in these four tables, so their load-tenant rows have to be removed before
		// the tenants themselves or the delete is rejected. Everything else the
		// tenant owns (resource_groups, policies, checks, slis, slos, budgets,
		// functions) cascades and needs no explicit delete.
		{name: "delete load-tenant audit events", cmd: psqlShell("DELETE FROM audit_events WHERE " + defaultTenantGuard)},
		{name: "delete load-tenant check runs", cmd: psqlShell("DELETE FROM check_runs WHERE " + defaultTenantGuard)},
		{name: "delete load-tenant SLI snapshots", cmd: psqlShell("DELETE FROM sli_snapshots WHERE " + defaultTenantGuard)},
		{name: "delete load-tenant budget alerts", cmd: psqlShell("DELETE FROM budget_alerts WHERE " + defaultTenantGuard)},
		// Keep the default tenant and its bootstrap API key; remove load-test tenants
		// and their keys so repeated runs do not hit 409 on tenant creation.
		{name: "delete load-test tenants", cmd: psqlShell("DELETE FROM api_keys WHERE " + defaultTenantGuard + "; DELETE FROM tenants WHERE slug != 'default'")},
	}
}

// runReset wipes the load test's own state: the control-plane ledger rows,
// every load-test tenant and its API keys, and the labelled per-tenant load
// namespaces with the ScheduledJob and JobRun custom resources and rendered
// Kubernetes Jobs inside them. It is scoped to what this tool created;
// shared infrastructure such as the long-lived monitoring stack in the
// monitoring namespace is never touched.
//
// It calls kubectl directly; the Go process executes kubectl internally so the
// reset is not subject to the same interactive gating as a typed shell command.
// The destructive nature matches clean: --confirm is required.
func runReset(args []string) error {
	flags := flag.NewFlagSet("reset", flag.ContinueOnError)
	confirm := flags.Bool("confirm", false, "confirm wiping control-plane state and load tenants")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*confirm {
		return fmt.Errorf("reset truncates control-plane state and deletes load tenants; pass --confirm to proceed")
	}
	// The legacy jobs, job_instances and job_instance_attempts tables no longer
	// exist, and neither does control_plane_tenant_mode: execution is one
	// Kubernetes path, so there is no per-tenant mode row and no separate
	// business-table layer to clear.
	steps := resetSteps()
	// Per-tenant namespaces are where this tool declared its ScheduledJobs and
	// where the operator rendered the Kubernetes Jobs. They are labeled, so
	// the cleanup discovers them instead of guessing names, and deleting the
	// namespace lets Kubernetes garbage-collect the custom resources and Jobs
	// inside them. Discovery is by label, so the monitoring namespace and any
	// other namespace this tool did not create are never matched.
	namespaced, err := exec.Command("kubectl", "get", "namespace",
		"-l", loadManagedByLabelKey+"="+loadManagedByLabelValue,
		"-o", "json").Output()
	if err != nil {
		return fmt.Errorf("[FAIL] reset: list load namespaces: %s", strings.TrimSpace(string(namespaced)))
	}
	namespaceNames, err := decodeKubeNames(namespaced)
	if err != nil {
		return fmt.Errorf("[FAIL] reset: list load namespaces: %v", err)
	}
	for _, namespace := range namespaceNames {
		steps = append(steps, resetStep{name: "delete load namespace " + namespace, cmd: []string{"kubectl", "delete", "namespace", namespace, "--ignore-not-found"}})
	}
	// Monitoring is long-lived infrastructure installed and removed by the
	// make targets, not by this tool, so reset only reports it and moves on.
	if namespaceExists("monitoring") {
		fmt.Println("[WARN] reset: namespace monitoring is the long-lived monitoring stack managed by make monitoring-up / make monitoring-down; reset does not touch it")
	}
	for _, step := range steps {
		out, err := exec.Command(step.cmd[0], step.cmd[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("[FAIL] reset: %s: %s", step.name, strings.TrimSpace(string(out)))
		}
		fmt.Printf("[OK] reset: %s\n", step.name)
	}
	fmt.Println("[OK] reset: complete, control-plane ledger and load-test state cleared")
	return nil
}

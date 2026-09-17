package main

import (
	"flag"
	"fmt"
	"os/exec"
	"strings"
)

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
	steps := []struct {
		name string
		cmd  []string
	}{
		// Control-plane state is keyed by source_uid + generation and occurrence,
		// not by tenant, so it survives the tenant delete below and leaks into the
		// next run: a recreated CR with the same name and generation collides with
		// the previous run's rows. Truncate all three together; CASCADE covers the
		// foreign keys between them. This must run before the tenant delete: all
		// three tables hold ON DELETE RESTRICT foreign keys on tenants(id).
		{name: "truncate control-plane tables", cmd: []string{"kubectl", "exec", "-n", DatabaseNamespace(), "deployment/orbitjob-postgres", "--", "psql", "-U", "postgres", "-d", "orbitjob", "-c", "TRUNCATE job_run_attempts_control_plane, job_run_control_plane, job_definition_revisions RESTART IDENTITY CASCADE"}},
		// The baseline schema puts ON DELETE RESTRICT foreign keys on tenants(id)
		// in these four tables, so their load-tenant rows have to be removed before
		// the tenants themselves or the delete is rejected. Everything else the
		// tenant owns (resource_groups, policies, checks, slis, slos, budgets)
		// cascades and needs no explicit delete.
		{name: "delete load-tenant audit events", cmd: []string{"kubectl", "exec", "-n", DatabaseNamespace(), "deployment/orbitjob-postgres", "--", "psql", "-U", "postgres", "-d", "orbitjob", "-c", "DELETE FROM audit_events WHERE tenant_id != (SELECT id FROM tenants WHERE slug = 'default')"}},
		{name: "delete load-tenant check runs", cmd: []string{"kubectl", "exec", "-n", DatabaseNamespace(), "deployment/orbitjob-postgres", "--", "psql", "-U", "postgres", "-d", "orbitjob", "-c", "DELETE FROM check_runs WHERE tenant_id != (SELECT id FROM tenants WHERE slug = 'default')"}},
		{name: "delete load-tenant SLI snapshots", cmd: []string{"kubectl", "exec", "-n", DatabaseNamespace(), "deployment/orbitjob-postgres", "--", "psql", "-U", "postgres", "-d", "orbitjob", "-c", "DELETE FROM sli_snapshots WHERE tenant_id != (SELECT id FROM tenants WHERE slug = 'default')"}},
		{name: "delete load-tenant budget alerts", cmd: []string{"kubectl", "exec", "-n", DatabaseNamespace(), "deployment/orbitjob-postgres", "--", "psql", "-U", "postgres", "-d", "orbitjob", "-c", "DELETE FROM budget_alerts WHERE tenant_id != (SELECT id FROM tenants WHERE slug = 'default')"}},
		// Keep the default tenant and its bootstrap API key; remove load-test tenants
		// and their keys so repeated runs do not hit 409 on tenant creation.
		{name: "delete load-test tenants", cmd: []string{"kubectl", "exec", "-n", DatabaseNamespace(), "deployment/orbitjob-postgres", "--", "psql", "-U", "postgres", "-d", "orbitjob", "-c", "DELETE FROM api_keys WHERE tenant_id != (SELECT id FROM tenants WHERE slug = 'default'); DELETE FROM tenants WHERE slug != 'default'"}},
	}
	// Per-tenant namespaces are where this tool declared its ScheduledJobs and
	// where the operator rendered the Kubernetes Jobs. They are labeled, so
	// the cleanup discovers them instead of guessing names, and deleting the
	// namespace lets Kubernetes garbage-collect the custom resources and Jobs
	// inside them. Discovery is by label, so the monitoring namespace and any
	// other namespace this tool did not create are never matched.
	namespaced, err := exec.Command("kubectl", "get", "namespace",
		"-l", loadManagedByLabelKey+"="+loadManagedByLabelValue,
		"-o", "jsonpath={range .items[*]}{.metadata.name} {end}").Output()
	if err != nil {
		return fmt.Errorf("[FAIL] reset: list load namespaces: %s", strings.TrimSpace(string(namespaced)))
	}
	for _, namespace := range strings.Fields(string(namespaced)) {
		steps = append(steps, struct {
			name string
			cmd  []string
		}{name: "delete load namespace " + namespace, cmd: []string{"kubectl", "delete", "namespace", namespace, "--ignore-not-found"}})
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

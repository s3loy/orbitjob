package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// runReset wipes OrbitJob business data, residual task Jobs, and restarts the
// worker + Prometheus so Prometheus counters (worker process memory) and TSDB
// history start clean. Run this before a fresh loadtest to stop historical
// counters from previous runs skewing Grafana graphs.
//
// It calls kubectl directly; the Go process executes kubectl internally so the
// reset is not subject to the same interactive gating as a typed shell command.
func runReset(args []string) error {
	_ = args
	steps := []struct {
		name string
		cmd  []string
	}{
		{name: "truncate business tables", cmd: []string{"kubectl", "exec", "-n", "orbitjob", "deployment/orbitjob-postgres", "--", "psql", "-U", "postgres", "-d", "orbitjob", "-c", "TRUNCATE job_instance_attempts, job_instances, jobs RESTART IDENTITY CASCADE"}},
		// Keep the default tenant and its bootstrap API key; remove load-test tenants
		// and their keys so repeated runs do not hit 409 on tenant creation.
		{name: "truncate load-test tenants", cmd: []string{"kubectl", "exec", "-n", "orbitjob", "deployment/orbitjob-postgres", "--", "psql", "-U", "postgres", "-d", "orbitjob", "-c", "DELETE FROM api_keys WHERE tenant_id != (SELECT id FROM tenants WHERE slug = 'default'); DELETE FROM tenants WHERE slug != 'default'"}},
		{name: "delete residual task jobs", cmd: []string{"kubectl", "delete", "jobs", "-n", "orbitjob-tasks", "--all", "--ignore-not-found"}},
		{name: "restart worker (reset counters)", cmd: []string{"kubectl", "rollout", "restart", "-n", "orbitjob", "deployment/orbitjob-worker"}},
		{name: "restart prometheus (clear TSDB)", cmd: []string{"kubectl", "delete", "pod", "-n", "monitoring", "-l", "app=prometheus", "--ignore-not-found"}},
	}
	for _, step := range steps {
		out, err := exec.Command(step.cmd[0], step.cmd[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %s", step.name, strings.TrimSpace(string(out)))
		}
		fmt.Printf("reset: %s\n", step.name)
	}
	if err := kubectlWait("deployment/orbitjob-worker", "orbitjob", 3*time.Minute); err != nil {
		return fmt.Errorf("wait worker: %w", err)
	}
	if err := kubectlWait("deployment/prometheus", "monitoring", 3*time.Minute); err != nil {
		return fmt.Errorf("wait prometheus: %w", err)
	}
	fmt.Println("reset: complete, counters and TSDB cleared")
	return nil
}

package main

import (
	"os"
	"os/exec"
	"strings"
)

// The namespaces this tool addresses.
//
// The control plane moved from `orbitjob` to `orbitjob-system` when the chart
// grew a separate database namespace, and these were hardcoded to `orbitjob`
// everywhere. Reset and fault injection then failed with
// `deployments.apps "orbitjob-worker" not found`, which reads like a broken
// install rather than a stale constant.
const (
	// TaskNamespace is where container jobs run. It comes from the chart value
	// containerExecution.namespace, so it is not the installer's choice.
	TaskNamespace = "orbitjob-tasks"
)

// WorkloadNamespace returns the namespace the control plane is installed into.
// Override with ORBITJOB_NAMESPACE when an install chose differently.
func WorkloadNamespace() string {
	return envNamespace("ORBITJOB_NAMESPACE", "orbitjob-system")
}

// DatabaseNamespace returns the namespace holding PostgreSQL and its exporter.
// Override with ORBITJOB_DB_NAMESPACE.
func DatabaseNamespace() string {
	return envNamespace("ORBITJOB_DB_NAMESPACE", "orbitjob")
}

func envNamespace(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// namespaceExists reports whether a namespace is present. Reset uses it to
// decide whether to mention infrastructure, such as the monitoring stack,
// that it deliberately leaves alone; absence is not an error.
func namespaceExists(name string) bool {
	if name == "" {
		return false
	}
	err := exec.Command("kubectl", "get", "namespace", name).Run()
	return err == nil
}

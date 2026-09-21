package main

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
)

// Cluster object names an install is expected to have. They follow from the
// chart: the release name prefixes the hook Jobs, and the components deploy
// under fixed app labels.
var installComponents = []string{"orbitjob-admin-api", "orbitjob-operator", "orbitjob-scheduler"}

// coreCRDs are the two Custom Resources the control plane cannot run without.
// The workflow CRDs ship in the chart but an install predating them stays
// valid, so their absence is a note, not a failure.
var coreCRDs = []string{
	"jobruns.workloads.orbitjob.io",
	"scheduledjobs.workloads.orbitjob.io",
}

var workflowCRDs = []string{
	"workflowjobs.workloads.orbitjob.io",
	"workflowruns.workloads.orbitjob.io",
}

const monitoringRelease = "kube-prometheus-stack"

type statusFlags struct {
	namespace    string
	release      string
	monitoringNS string
}

// CheckLine is one diagnostic finding, printed as "<status> <detail>" and
// shared by status and doctor.
type CheckLine struct {
	Status string `json:"status"` // [OK], [FAIL], WARN
	Detail string `json:"detail"`
	Failed bool   `json:"-"`
}

// statusCommand prints the install snapshot from kubectl only.
func statusCommand(ctx context.Context, args []string) error {
	usageLine := "orbitjob status [--namespace NS] [--release NAME] [--monitoring-namespace NS] [--json]"
	var conn connFlags
	var flags statusFlags
	fs := flagSet(usageLine)
	conn.add(fs)
	fs.StringVar(&flags.namespace, "namespace", "orbitjob-system", "namespace the chart release lives in")
	fs.StringVar(&flags.release, "release", "orbitjob", "helm release name of the install")
	fs.StringVar(&flags.monitoringNS, "monitoring-namespace", "monitoring", "namespace the monitoring release lives in")
	help, err := parseArgs(fs, args, usageLine)
	if err != nil {
		return err
	}
	if help {
		return nil
	}

	lines, failed := inspectInstall(ctx, newKubeRunner(), flags)
	if conn.json {
		return emitJSON(lines)
	}
	for _, l := range lines {
		_, _ = fmt.Fprintf(stdoutWriter, "%s %s\n", l.Status, l.Detail)
	}
	if failed {
		return failf(exitGeneral, "status found failures")
	}
	return nil
}

// inspectInstall gathers every status line and reports whether any check
// failed. Each line stands alone so one missing piece does not hide the rest.
func inspectInstall(ctx context.Context, kube kubeRunner, flags statusFlags) ([]CheckLine, bool) {
	var lines []CheckLine
	failed := false
	add := func(status, detail string) {
		if status == "[FAIL]" {
			failed = true
		}
		lines = append(lines, CheckLine{Status: status, Detail: detail, Failed: status == "[FAIL]"})
	}

	// Helm release state lives in labeled secrets; their names carry the
	// revision (sh.helm.release.v1.<release>.v<rev>).
	revision, ok := helmReleaseRevision(ctx, kube, flags.namespace, flags.release)
	if ok {
		add("[OK]", fmt.Sprintf("helm release %s revision %d in namespace %s", flags.release, revision, flags.namespace))
	} else {
		add("[FAIL]", fmt.Sprintf("helm release %s not found in namespace %s", flags.release, flags.namespace))
	}

	for _, component := range installComponents {
		ready, total, restarts := deploymentState(ctx, kube, flags.namespace, component)
		switch {
		case total == 0:
			add("[FAIL]", fmt.Sprintf("deployment %s: not found in namespace %s", component, flags.namespace))
		case ready == total:
			add("[OK]", fmt.Sprintf("deployment %s: %d/%d ready, %d restarts", component, ready, total, restarts))
		default:
			add("[FAIL]", fmt.Sprintf("deployment %s: %d/%d ready, %d restarts", component, ready, total, restarts))
		}
	}

	version, ok := shippedSchemaVersion(ctx, kube, flags.namespace, flags.release)
	if ok {
		add("[OK]", fmt.Sprintf("schema migrations shipped: version %d (configmap %s-migrations)", version, flags.release))
	} else {
		add("[FAIL]", fmt.Sprintf("schema migrations: configmap %s-migrations missing or unreadable in namespace %s", flags.release, flags.namespace))
	}

	for _, crd := range coreCRDs {
		present, err := resourceExists(ctx, kube, "get", "crd", crd)
		if err != nil {
			add("[FAIL]", fmt.Sprintf("crd %s: %v", crd, err))
			continue
		}
		if present {
			add("[OK]", fmt.Sprintf("crd %s: present", crd))
		} else {
			add("[FAIL]", fmt.Sprintf("crd %s: missing", crd))
		}
	}
	for _, crd := range workflowCRDs {
		present, err := resourceExists(ctx, kube, "get", "crd", crd)
		if err != nil {
			add("WARN", fmt.Sprintf("crd %s: %v", crd, err))
			continue
		}
		if present {
			add("[OK]", fmt.Sprintf("crd %s: present", crd))
		} else {
			add("WARN", fmt.Sprintf("crd %s: not installed (workflow support)", crd))
		}
	}

	present, err := resourceExists(ctx, kube, "get", "secrets", "-n", flags.monitoringNS, "-l", "owner=helm,name="+monitoringRelease)
	switch {
	case err != nil:
		add("WARN", fmt.Sprintf("monitoring release %s: %v", monitoringRelease, err))
	case present:
		add("[OK]", fmt.Sprintf("monitoring release %s present in namespace %s", monitoringRelease, flags.monitoringNS))
	default:
		add("WARN", fmt.Sprintf("monitoring release %s not found in namespace %s", monitoringRelease, flags.monitoringNS))
	}

	return lines, failed
}

// helmReleaseRevision reads the newest revision of a release from its helm
// state secret. Missing or unreadable both mean "no release we can see".
func helmReleaseRevision(ctx context.Context, kube kubeRunner, namespace, release string) (int, bool) {
	decoded, err := kubeJSON(ctx, kube, "get", "secrets", "-n", namespace, "-l", "owner=helm,name="+release)
	if err != nil {
		return 0, false
	}
	items, _ := decoded["items"].([]any)
	best := 0
	for _, item := range items {
		obj, _ := item.(map[string]any)
		meta, _ := obj["metadata"].(map[string]any)
		if meta == nil {
			continue
		}
		name, _ := meta["name"].(string)
		// sh.helm.release.v1.<release>.v<revision>
		m := releaseSecretRevision.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		if rev, err := strconv.Atoi(m[1]); err == nil && rev > best {
			best = rev
		}
	}
	return best, best > 0
}

var releaseSecretRevision = regexp.MustCompile(`\.v(\d+)$`)

// deploymentState reads desired/ready replicas and summed restart counts for
// one component's pods.
func deploymentState(ctx context.Context, kube kubeRunner, namespace, component string) (ready, total, restarts int) {
	decoded, err := kubeJSON(ctx, kube, "get", "deployment", component, "-n", namespace)
	if err != nil {
		return 0, 0, 0
	}
	status, _ := decoded["status"].(map[string]any)
	spec, _ := decoded["spec"].(map[string]any)
	desired := jsonInt(spec["replicas"])
	readyReplicas := jsonInt(status["readyReplicas"])
	if replicas, ok := status["replicas"]; ok {
		// A deployment mid-rollout reports the old scale under status.replicas.
		if r := jsonInt(replicas); r > desired {
			desired = r
		}
	}
	if desired == 0 {
		desired = 1
	}
	restarts = podRestarts(ctx, kube, namespace, component)
	return readyReplicas, desired, restarts
}

// podRestarts sums container restarts across the component's pods.
func podRestarts(ctx context.Context, kube kubeRunner, namespace, component string) int {
	decoded, err := kubeJSON(ctx, kube, "get", "pods", "-n", namespace, "-l", "app.kubernetes.io/name="+component)
	if err != nil {
		return 0
	}
	items, _ := decoded["items"].([]any)
	restarts := 0
	for _, item := range items {
		obj, _ := item.(map[string]any)
		status, _ := obj["status"].(map[string]any)
		containers, _ := status["containerStatuses"].([]any)
		for _, c := range containers {
			container, _ := c.(map[string]any)
			restarts += jsonInt(container["restartCount"])
		}
	}
	return restarts
}

// shippedSchemaVersion derives the applied schema_migrations version from the
// release's migrations ConfigMap: its data keys are the migration files the
// pre-install hook Job runs. kubectl is the only channel a client has to the
// database's applied version, and the ConfigMap is the install's own record.
func shippedSchemaVersion(ctx context.Context, kube kubeRunner, namespace, release string) (int, bool) {
	decoded, err := kubeJSON(ctx, kube, "get", "configmap", release+"-migrations", "-n", namespace)
	if err != nil {
		return 0, false
	}
	data, _ := decoded["data"].(map[string]any)
	names := make([]string, 0, len(data))
	for name := range data {
		names = append(names, name)
	}
	sort.Strings(names)
	best := 0
	for _, name := range names {
		if m := migrationFileVersion.FindStringSubmatch(name); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil && v > best {
				best = v
			}
		}
	}
	return best, best > 0
}

var migrationFileVersion = regexp.MustCompile(`^0*(\d+)_.*\.up\.sql$`)

// jsonInt tolerates the float64 json decoding gives numbers.
func jsonInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// doctorCommand is the diagnostic bundle: the API auth path end to end, the
// install snapshot, per-namespace RBAC, the leader lease and restart counts.
// It exits non-zero when any line fails; a WARN never fails it.
func doctorCommand(ctx context.Context, args []string) error {
	usageLine := "orbitjob doctor [--namespace NS] [--release NAME] [--namespaces NS1,NS2] [--monitoring-namespace NS] [--api-url URL] [--api-key KEY] [--json]"
	var conn connFlags
	var namespace, release, monitoringNS, tenantNamespaces string
	fs := flagSet(usageLine)
	conn.add(fs)
	fs.StringVar(&namespace, "namespace", "orbitjob-system", "namespace the chart release lives in")
	fs.StringVar(&release, "release", "orbitjob", "helm release name of the install")
	fs.StringVar(&monitoringNS, "monitoring-namespace", "monitoring", "namespace the monitoring release lives in")
	fs.StringVar(&tenantNamespaces, "namespaces", "", "comma-separated tenant namespaces to check RBAC in (default: discover from the operator deployment)")
	help, err := parseArgs(fs, args, usageLine)
	if err != nil {
		return err
	}
	if help {
		return nil
	}

	kube := newKubeRunner()
	var lines []CheckLine
	failed := false
	add := func(status, detail string) {
		if status == "[FAIL]" {
			failed = true
		}
		lines = append(lines, CheckLine{Status: status, Detail: detail})
	}

	// The auth path first: everything API-shaped below depends on it.
	key, keySrc, keyErr := resolveDoctorKey(ctx, kube, namespace, conn.apiKey)
	if keyErr != nil {
		add("[FAIL]", fmt.Sprintf("API key: %v", keyErr))
	} else {
		add("[OK]", fmt.Sprintf("API key: resolved from %s", keySrc))
	}

	if keyErr == nil {
		client := &apiClient{base: conn.resolveURL(), key: key}
		tenants, err := client.do(ctx, http.MethodGet, "/api/v1/tenants", nil, nil)
		if err != nil {
			if apiErr, ok := apiErrorOf(err); ok {
				add("[FAIL]", fmt.Sprintf("admin API: /api/v1/tenants returned %d %s: %s", apiErr.status, apiErr.code, apiErr.message))
			} else {
				add("[FAIL]", err.Error())
			}
		} else {
			add("[OK]", fmt.Sprintf("admin API: /api/v1/tenants 200 (%s)", summarizeTenants(tenants)))
		}
	}

	// The cluster side: install snapshot (release, deployments, schema, CRDs,
	// monitoring) shares status's inspection.
	installLines, installFailed := inspectInstall(ctx, kube, statusFlags{
		namespace: namespace, release: release, monitoringNS: monitoringNS,
	})
	lines = append(lines, installLines...)
	if installFailed {
		failed = true
	}

	if strings.TrimSpace(tenantNamespaces) == "" {
		discovered, err := discoverTenantNamespaces(ctx, kube, namespace)
		if err != nil {
			add("[FAIL]", fmt.Sprintf("tenant namespaces: %v", err))
		} else {
			tenantNamespaces = strings.Join(discovered, ",")
			add("[OK]", fmt.Sprintf("tenant namespaces: discovered %s", tenantNamespaces))
		}
	}

	// Leader lease: the singleton loops must have a holder.
	holder, leaseErr := leaseHolder(ctx, kube, namespace, release)
	if leaseErr != nil {
		add("[FAIL]", fmt.Sprintf("leader lease: %v", leaseErr))
	} else if holder == "" {
		add("[FAIL]", fmt.Sprintf("leader lease orbitjob-operator-singleton: present in namespace %s but no holder", namespace))
	} else {
		add("[OK]", fmt.Sprintf("leader lease orbitjob-operator-singleton: held by %s", holder))
	}

	// RBAC: what the current kubeconfig identity may do to the run custom
	// resources in each tenant namespace. Workflow resources are checked only
	// when their CRD is installed.
	for _, ns := range splitNamespaces(tenantNamespaces) {
		for _, verb := range []string{"create", "list"} {
			allowed, err := canI(ctx, kube, verb, "jobruns.workloads.orbitjob.io", ns)
			if err != nil {
				add("[FAIL]", fmt.Sprintf("RBAC %s: can-i %s jobruns: %v", ns, verb, err))
				continue
			}
			if allowed {
				add("[OK]", fmt.Sprintf("RBAC %s: can-i %s jobruns.workloads.orbitjob.io", ns, verb))
			} else {
				add("[FAIL]", fmt.Sprintf("RBAC %s: can-i %s jobruns.workloads.orbitjob.io = no", ns, verb))
			}
		}
		for _, resource := range []string{"workflowjobs.workloads.orbitjob.io", "workflowruns.workloads.orbitjob.io"} {
			present, err := resourceExists(ctx, kube, "get", "crd", resource)
			if err != nil {
				add("WARN", fmt.Sprintf("RBAC %s: %v", ns, err))
				continue
			}
			if !present {
				add("WARN", fmt.Sprintf("RBAC %s: %s not installed, can-i skipped", ns, resource))
				continue
			}
			allowed, err := canI(ctx, kube, "list", resource, ns)
			if err != nil {
				add("[FAIL]", fmt.Sprintf("RBAC %s: can-i list %s: %v", ns, resource, err))
				continue
			}
			if allowed {
				add("[OK]", fmt.Sprintf("RBAC %s: can-i list %s", ns, resource))
			} else {
				add("[FAIL]", fmt.Sprintf("RBAC %s: can-i list %s = no", ns, resource))
			}
		}
	}

	if conn.json {
		return emitJSON(lines)
	}
	for _, l := range lines {
		_, _ = fmt.Fprintf(stdoutWriter, "%s %s\n", l.Status, l.Detail)
	}
	if failed {
		return failf(exitGeneral, "doctor found failures; see the [FAIL] lines above")
	}
	return nil
}

// apiBaseFor resolves the API base the same way every other command does, so
// doctor cannot disagree with runs list about where the server is.

// resolveDoctorKey finds the API key by flag, then environment, then the
// bootstrap secret the install wrote it to. Only the source is reported; the
// value stays inside the client.
func resolveDoctorKey(ctx context.Context, kube kubeRunner, namespace, flagKey string) (string, keySource, error) {
	if flagKey != "" {
		return flagKey, keyFromFlag, nil
	}
	if envKey := strings.TrimSpace(os.Getenv(apiKeyEnvVar)); envKey != "" {
		return envKey, keyFromEnv, nil
	}
	decoded, err := kubeJSON(ctx, kube, "get", "secret", "bootstrap-api-key", "-n", namespace)
	if err != nil {
		return "", "", fmt.Errorf("no --api-key, no %s, and secret bootstrap-api-key unreadable in namespace %s (%v) -- source <(make kind-env) first", apiKeyEnvVar, namespace, err)
	}
	var secret struct {
		Data map[string][]byte `json:"data"`
	}
	raw, _ := json.Marshal(decoded)
	if err := json.Unmarshal(raw, &secret); err != nil {
		return "", "", fmt.Errorf("parse secret bootstrap-api-key: %v", err)
	}
	key := strings.TrimSpace(string(secret.Data["api-key"]))
	if key == "" {
		return "", "", fmt.Errorf("secret bootstrap-api-key in namespace %s has no api-key entry", namespace)
	}
	return key, keyFromSecret, nil
}

// leaseHolder reads the operator's Lease and returns its holderIdentity.
func leaseHolder(ctx context.Context, kube kubeRunner, namespace, release string) (string, error) {
	decoded, err := kubeJSON(ctx, kube, "get", "lease", "orbitjob-operator-singleton", "-n", namespace)
	if err != nil {
		return "", fmt.Errorf("lease orbitjob-operator-singleton not found in namespace %s", namespace)
	}
	spec, _ := decoded["spec"].(map[string]any)
	holder, _ := spec["holderIdentity"].(string)
	return holder, nil
}

// canI asks the cluster what the current identity may do.
func canI(ctx context.Context, kube kubeRunner, verb, resource, namespace string) (bool, error) {
	out, err := kube.run(ctx, "auth", "can-i", verb, resource, "-n", namespace)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "yes", nil
}

func splitNamespaces(list string) []string {
	var out []string
	for _, part := range strings.Split(list, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// discoverTenantNamespaces reads the exact namespace keys the running operator
// accepts. Keeping doctor tied to the deployed mapping avoids checking a stale
// conventional namespace that the operator does not watch.
func discoverTenantNamespaces(ctx context.Context, kube kubeRunner, namespace string) ([]string, error) {
	decoded, err := kubeJSON(ctx, kube, "get", "deployment", "orbitjob-operator", "-n", namespace)
	if err != nil {
		return nil, fmt.Errorf("read operator deployment in namespace %s: %w", namespace, err)
	}
	spec, _ := decoded["spec"].(map[string]any)
	template, _ := spec["template"].(map[string]any)
	podSpec, _ := template["spec"].(map[string]any)
	containers, _ := podSpec["containers"].([]any)
	for _, item := range containers {
		container, _ := item.(map[string]any)
		env, _ := container["env"].([]any)
		for _, raw := range env {
			entry, _ := raw.(map[string]any)
			if entry["name"] != "OPERATOR_NAMESPACE_TENANTS" {
				continue
			}
			value, _ := entry["value"].(string)
			var namespaces []string
			for _, mapping := range strings.Split(value, ",") {
				parts := strings.SplitN(strings.TrimSpace(mapping), "=", 2)
				if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
					return nil, fmt.Errorf("operator deployment has malformed OPERATOR_NAMESPACE_TENANTS")
				}
				namespaces = append(namespaces, strings.TrimSpace(parts[0]))
			}
			if len(namespaces) == 0 {
				return nil, fmt.Errorf("operator deployment has empty OPERATOR_NAMESPACE_TENANTS")
			}
			return namespaces, nil
		}
	}
	return nil, fmt.Errorf("operator deployment has no OPERATOR_NAMESPACE_TENANTS")
}

// summarizeTenants reduces the tenants body to "N tenants" style detail.
func summarizeTenants(body []byte) string {
	items, err := decodeList[struct {
		ID string `json:"id"`
	}](body)
	if err != nil || len(items) == 0 {
		return "no tenants visible"
	}
	return fmt.Sprintf("%d tenants", len(items))
}

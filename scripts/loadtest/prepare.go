package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type kubeObject struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Spec map[string]any `yaml:"spec"`
}

func ValidateLoadManifests(namespacePath, rbacPath string) error {
	data, err := os.ReadFile(namespacePath)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	quotas := map[string]map[string]string{}
	for {
		var object kubeObject
		if err := decoder.Decode(&object); err != nil {
			break
		}
		if object.Kind != "ResourceQuota" {
			continue
		}
		hard, _ := object.Spec["hard"].(map[string]any)
		values := map[string]string{}
		for key, value := range hard {
			values[key] = fmt.Sprint(value)
		}
		quotas[object.Metadata.Namespace] = values
	}
	if quotas["orbitjob-load"]["limits.cpu"] != "1" || quotas["orbitjob-load"]["limits.memory"] != "1Gi" {
		return fmt.Errorf("orbitjob-load quota does not match 1 CPU / 1Gi")
	}
	if quotas["orbitjob-tasks"]["limits.cpu"] != "10" || quotas["orbitjob-tasks"]["limits.memory"] != "8Gi" {
		return fmt.Errorf("orbitjob-tasks quota does not match 10 CPU / 8Gi")
	}
	rbac, err := os.ReadFile(rbacPath)
	if err != nil {
		return err
	}
	if strings.Contains(string(rbac), "\"secrets\"") || strings.Contains(string(rbac), "- secrets") {
		return fmt.Errorf("operations RBAC must not access secrets")
	}
	return nil
}

// CreatedDefinition records the mapping from a generated case to the
// definition revision the operator materialized from its ScheduledJob custom
// resource. The revision id is what a manual trigger addresses.
type CreatedDefinition struct {
	CaseID string `json:"case_id"`
	// RevisionID is the active revision the operator projected from the
	// declared ScheduledJob, read back from status.activeRevision.
	RevisionID int64  `json:"revision_id"`
	Tenant     string `json:"tenant"`
	// ExpectedTerminalState is what the scenario says this definition should
	// end as. The run engine reads it to decide whether it needs to do
	// something -- a scenario expecting "canceled" is only ever canceled if
	// the load generator issues the cancel, and nothing did.
	ExpectedTerminalState string `json:"expected_terminal_state,omitempty"`
}

// scheduledJobAPIVersion is the Custom Resource version the load tool declares
// definitions with. It must match the operator's watched group version.
const scheduledJobAPIVersion = "workloads.orbitjob.io/v1alpha1"

// loadManagedByLabelKey and loadManagedByValue label every namespace and
// custom resource the load tool creates, so reset can find and remove them
// without hardcoding a namespace list.
const (
	loadManagedByLabelKey   = "orbitjob.io/managed-by"
	loadManagedByLabelValue = "orbitjob-loadtest"
)

// TenantNamespace derives the namespace a tenant's definitions are declared
// in. Tenancy comes from the operator's namespace table, so one tenant means
// one namespace; the name is derived from the tenant id so prepare is
// deterministic across reruns and never depends on a slug.
func TenantNamespace(tenantID string) string {
	return "orbitjob-tasks-" + strings.ToLower(tenantID)
}

// TenantSlug derives the tenant row's slug from the tenant id. The slug is a
// label, not an identifier -- the id is the ULID the config declares -- but
// the tenants table requires a unique one, and deriving it from the id keeps
// repeated prepares from colliding.
func TenantSlug(tenantID string) string {
	return "load-" + strings.ToLower(tenantID)
}

// Prepare declares every generated definition as a ScheduledJob custom
// resource and records the revision ids the operator materializes. It also
// provisions everything the run depends on: tenant rows with the config's
// fixed ULIDs, one watched namespace per tenant, the operator's namespace
// table, and tenant API keys. Tenant keys are written with 0600 permissions.
//
// Prepare is idempotent rather than resume-capable: reapplying an unchanged
// ScheduledJob keeps its revision, so a rerun re-reads the same ids, and the
// definitions file is rewritten from what the cluster reports rather than
// trusted from a previous attempt.
func Prepare(configPath, imagesPath, runID, runRoot, apiURL, bootstrapKey, profile string) error {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := validateProfile(profile, cfg); err != nil {
		return fmt.Errorf("validate profile: %w", err)
	}
	if err := ValidateLoadManifests("deploy/load/namespace.yaml", "deploy/load/operations-rbac.yaml"); err != nil {
		return fmt.Errorf("validate manifests: %w", err)
	}
	if _, err := LoadImageLock(imagesPath); err != nil {
		return fmt.Errorf("load image lock: %w", err)
	}

	runDir := filepath.Join(runRoot, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("create run dir: %w", err)
	}

	manifests := []string{"namespace.yaml", "operations-rbac.yaml", "fixture-configmap.yaml", "fixture-tls-secret.yaml", "fixture.yaml", "postgres.yaml"}
	for _, name := range manifests {
		if err := kubectlApply(filepath.Join("deploy/load", name)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	if err := kubectlWait("deployment/load-fixture", "orbitjob-load", 3*time.Minute); err != nil {
		return fmt.Errorf("wait fixture: %w", err)
	}
	if err := kubectlWait("deployment/load-postgres", "orbitjob-load", 3*time.Minute); err != nil {
		return fmt.Errorf("wait load postgres: %w", err)
	}

	defs, err := loadGeneratedDefinitions(filepath.Join(runDir, "generated", "definitions.json"))
	if err != nil {
		return fmt.Errorf("load generated definitions: %w", err)
	}

	bootstrap := NewAPIClient(apiURL, bootstrapKey)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Tenants exist with the config's fixed ULIDs before anything references
	// them: the namespace table names these ids, so a missing tenant row would
	// leave the operator resolving CRs into a tenant that does not exist.
	if err := seedTenants(ctx, cfg.Tenants); err != nil {
		return fmt.Errorf("seed tenants: %w", err)
	}

	// One watched namespace per tenant, carrying the same capacity shape the
	// shared task namespace has, because this is now where the rendered
	// Kubernetes Jobs execute.
	for _, tenantID := range cfg.Tenants {
		if err := ensureTenantNamespace(ctx, tenantID); err != nil {
			return fmt.Errorf("namespace for tenant %s: %w", tenantID, err)
		}
	}

	// The operator only resolves namespaces its table maps. Extend the table
	// in place rather than replacing it, so the rest of the installation keeps
	// the mappings it was installed with.
	if err := watchTenantNamespaces(ctx, cfg.Tenants); err != nil {
		return fmt.Errorf("operator namespace table: %w", err)
	}

	// Declare the corpus: one ScheduledJob per generated definition, batched
	// into one multi-document manifest per tenant so a 1200-definition corpus
	// costs four applies, not 1200.
	manifestDir := filepath.Join(runDir, "cr-manifests")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return fmt.Errorf("create manifest dir: %w", err)
	}
	byTenant := make(map[string][]Definition, len(cfg.Tenants))
	for _, def := range defs {
		byTenant[def.Tenant] = append(byTenant[def.Tenant], def)
	}
	for _, tenantID := range cfg.Tenants {
		// kubectl reads one object per YAML document; a top-level sequence is
		// not a manifest, so the batch is written as --- separated documents.
		objects := scheduledJobManifests(byTenant[tenantID], tenantID)
		docs := make([][]byte, 0, len(objects))
		for _, object := range objects {
			doc, err := yaml.Marshal(object)
			if err != nil {
				return fmt.Errorf("render manifest for tenant %s: %w", tenantID, err)
			}
			docs = append(docs, doc)
		}
		path := filepath.Join(manifestDir, strings.ToLower(tenantID)+".yaml")
		if err := os.WriteFile(path, []byte(bytes.Join(docs, []byte("---\n"))), 0o600); err != nil {
			return fmt.Errorf("write manifest: %w", err)
		}
		if err := kubectlApply(path); err != nil {
			return fmt.Errorf("apply definitions for tenant %s: %w", tenantID, err)
		}
	}

	// The trigger route addresses revisions, and the revision appears when the
	// operator projects the CR, so wait until every declared definition has an
	// active revision and record exactly what the cluster reports.
	revisions, err := waitRevisions(ctx, cfg.Tenants, defs)
	if err != nil {
		return err
	}
	created := make([]CreatedDefinition, 0, len(defs))
	for _, def := range defs {
		created = append(created, CreatedDefinition{
			CaseID:                def.CaseID,
			RevisionID:            revisions[def.CaseID],
			Tenant:                def.Tenant,
			ExpectedTerminalState: def.Expected.TerminalState,
		})
	}
	if err := writeStableJSON(filepath.Join(runDir, "created-definitions.json"), created); err != nil {
		return fmt.Errorf("write created definitions: %w", err)
	}
	namespaces := make(map[string]string, len(cfg.Tenants))
	for _, tenantID := range cfg.Tenants {
		namespaces[tenantID] = TenantNamespace(tenantID)
	}
	if err := writeStableJSON(filepath.Join(runDir, "tenant-namespaces.json"), namespaces); err != nil {
		return fmt.Errorf("write tenant namespaces: %w", err)
	}

	// Tenant API keys. Keys from a previous prepare attempt are reused when
	// they still validate, so a rerun does not mint credentials it will never
	// use. The key determines the tenant on every call the run engine makes.
	var policyID string
	if err := withBackoff(ctx, func() error {
		var err error
		policyID, err = bootstrap.PolicyIDByName(ctx, TenantAdminPolicyName)
		return err
	}); err != nil {
		return fmt.Errorf("resolve %s: %w", TenantAdminPolicyName, err)
	}
	tenantKeys := map[string]string{}
	if existing, err := loadTenantKeys(filepath.Join(runDir, "tenant-keys.json")); err == nil {
		tenantKeys = existing
	}
	for _, tenantID := range cfg.Tenants {
		if key, ok := tenantKeys[tenantID]; ok {
			if _, err := NewAPIClient(apiURL, key).ListInstances(ctx, 1); err == nil {
				continue
			}
			delete(tenantKeys, tenantID)
		}
		var key string
		if err := withBackoff(ctx, func() error {
			var err error
			key, err = bootstrap.CreateAPIKey(ctx, tenantID, []string{policyID})
			return err
		}); err != nil {
			return fmt.Errorf("create api key %s: %w", tenantID, err)
		}
		tenantKeys[tenantID] = key
	}
	if err := writeStableJSON(filepath.Join(runDir, "tenant-keys.json"), tenantKeys); err != nil {
		return fmt.Errorf("write tenant keys: %w", err)
	}
	fmt.Printf("prepare: declared %d definitions across %d tenants\n", len(created), len(tenantKeys))
	return nil
}

// scheduledJobManifests renders one ScheduledJob object per definition, all in
// the tenant's namespace.
func scheduledJobManifests(defs []Definition, tenantID string) []kubeObject {
	namespace := TenantNamespace(tenantID)
	objects := make([]kubeObject, 0, len(defs))
	for _, def := range defs {
		var object kubeObject
		object.APIVersion = scheduledJobAPIVersion
		object.Kind = "ScheduledJob"
		object.Metadata.Name = def.CaseID
		object.Metadata.Namespace = namespace
		object.Spec = map[string]any{
			"schedule": def.ScheduledJob.Schedule,
			"history": map[string]any{
				"successfulRuns": def.ScheduledJob.HistorySuccessful,
				"failedRuns":     def.ScheduledJob.HistoryFailed,
			},
			"jobTemplate": map[string]any{
				"image":        def.ScheduledJob.Image,
				"command":      def.ScheduledJob.Command,
				"args":         def.ScheduledJob.Args,
				"backoffLimit": def.ScheduledJob.BackoffLimit,
			},
			"timeoutSeconds": def.ScheduledJob.TimeoutSeconds,
		}
		objects = append(objects, object)
	}
	return objects
}

// seedTenants inserts any missing tenant rows with the config's ULIDs. The
// API cannot mint a tenant under a chosen id -- the server generates one --
// and the namespace table needs these exact ids, so the rows are written the
// way reset cleans them: with psql in the installation database. INSERT ..
// ON CONFLICT DO NOTHING makes the step idempotent; the count check after it
// makes it honest.
func seedTenants(ctx context.Context, tenants []string) error {
	for _, tenantID := range tenants {
		slug := TenantSlug(tenantID)
		sql := fmt.Sprintf(
			"INSERT INTO tenants (id, slug, name, status) VALUES ('%s', '%s', '%s', 'active') ON CONFLICT DO NOTHING",
			tenantID, slug, slug)
		if out, err := exec.CommandContext(ctx, "kubectl", "exec", "-n", DatabaseNamespace(),
			"deployment/orbitjob-postgres", "--", "psql", "-U", "postgres", "-d", "orbitjob", "-c", sql).CombinedOutput(); err != nil {
			return fmt.Errorf("insert tenant %s: %s", tenantID, strings.TrimSpace(string(out)))
		}
	}
	check := fmt.Sprintf("SELECT count(*) FROM tenants WHERE id IN (%s)", quotedList(tenants))
	out, err := exec.CommandContext(ctx, "kubectl", "exec", "-n", DatabaseNamespace(),
		"deployment/orbitjob-postgres", "--", "psql", "-U", "postgres", "-d", "orbitjob", "-Atqc", check).CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify tenants: %s", strings.TrimSpace(string(out)))
	}
	var found int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &found); err != nil {
		return fmt.Errorf("verify tenants: psql answered %q", strings.TrimSpace(string(out)))
	}
	if found != len(tenants) {
		return fmt.Errorf("verify tenants: %d of %d tenant rows present", found, len(tenants))
	}
	return nil
}

func quotedList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf("'%s'", value))
	}
	return strings.Join(quoted, ",")
}

// ensureTenantNamespace creates the tenant's namespace with the same capacity
// shape as the shared task namespace: this is where the operator renders the
// Kubernetes Jobs for the tenant's runs, so the quota and limit range that
// bounded the old shared namespace bound this one.
func ensureTenantNamespace(ctx context.Context, tenantID string) error {
	namespace := TenantNamespace(tenantID)
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    %s: %s
---
apiVersion: v1
kind: ResourceQuota
metadata:
  name: %s-tasks
  namespace: %s
spec:
  hard:
    requests.cpu: 5
    requests.memory: 5Gi
    limits.cpu: "10"
    limits.memory: 8Gi
---
apiVersion: v1
kind: LimitRange
metadata:
  name: %s-task-defaults
  namespace: %s
spec:
  limits:
    - type: Container
      defaultRequest: {cpu: 10m, memory: 16Mi}
      default: {cpu: 500m, memory: 256Mi}
      max: {cpu: 500m, memory: 256Mi}
`, namespace, loadManagedByLabelKey, loadManagedByLabelValue, namespace, namespace, namespace, namespace)
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// watchTenantNamespaces extends the operator's namespace-to-tenant table with
// one entry per load tenant and restarts the operator only when the table
// changed. Existing entries are preserved: the installation may map other
// namespaces to other tenants, and this step has no mandate over them.
func watchTenantNamespaces(ctx context.Context, tenants []string) error {
	existing, err := currentNamespaceTenants(ctx)
	if err != nil {
		return err
	}
	merged := map[string]string{}
	for _, pair := range strings.Split(existing, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("operator namespace table has malformed entry %q", pair)
		}
		merged[parts[0]] = parts[1]
	}
	changed := false
	for _, tenantID := range tenants {
		namespace := TenantNamespace(tenantID)
		if merged[namespace] != tenantID {
			merged[namespace] = tenantID
			changed = true
		}
	}
	if !changed {
		return nil
	}
	pairs := make([]string, 0, len(merged))
	for namespace, tenant := range merged {
		pairs = append(pairs, namespace+"="+tenant)
	}
	sort.Strings(pairs)
	value := strings.Join(pairs, ",")
	if out, err := exec.CommandContext(ctx, "kubectl", "set", "env", "deployment/orbitjob-operator",
		"-n", WorkloadNamespace(), "OPERATOR_NAMESPACE_TENANTS="+value).CombinedOutput(); err != nil {
		return fmt.Errorf("set operator namespace table: %s", strings.TrimSpace(string(out)))
	}
	if err := kubectlRolloutRestart("deployment/orbitjob-operator", WorkloadNamespace()); err != nil {
		return fmt.Errorf("restart operator: %w", err)
	}
	return kubectlWait("deployment/orbitjob-operator", WorkloadNamespace(), 3*time.Minute)
}

// currentNamespaceTenants reads OPERATOR_NAMESPACE_TENANTS from the operator's
// deployment. An absent variable reads as empty: the chart requires the value
// at install time, but reading what is actually configured beats assuming it.
func currentNamespaceTenants(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "kubectl", "get", "deployment", "orbitjob-operator",
		"-n", WorkloadNamespace(), "-o", "json").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("read operator deployment: %s", strings.TrimSpace(string(out)))
	}
	var deployment struct {
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Env []struct {
							Name  string `json:"name"`
							Value string `json:"value"`
						} `json:"env"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(out, &deployment); err != nil {
		return "", fmt.Errorf("decode operator deployment: %w", err)
	}
	for _, container := range deployment.Spec.Template.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == "OPERATOR_NAMESPACE_TENANTS" {
				return env.Value, nil
			}
		}
	}
	return "", nil
}

// waitRevisions polls every tenant namespace until each declared ScheduledJob
// reports an active revision, and returns case id -> revision id. A CR with no
// revision after the deadline means the operator did not accept the
// declaration, which is a prepare failure, not a run-time surprise.
//
// The read is plain JSON, not a jsonpath template: a runner's kubectl renders
// jsonpath inconsistently enough that a template this repo shipped once read
// as empty on GitHub's runner while the same cluster answered the same query
// in JSON. Structured decoding has no such variance.
func waitRevisions(ctx context.Context, tenants []string, defs []Definition) (map[string]int64, error) {
	declared := make(map[string]string, len(defs)) // case id -> tenant
	for _, def := range defs {
		declared[def.CaseID] = def.Tenant
	}
	deadline := time.Now().Add(10 * time.Minute)
	revisions := map[string]int64{}
	for {
		for _, tenantID := range tenants {
			revisionsIn, err := scheduledJobRevisions(ctx, TenantNamespace(tenantID))
			if err != nil {
				return nil, fmt.Errorf("read scheduled jobs for tenant %s: %w", tenantID, err)
			}
			for name, rev := range revisionsIn {
				if rev > 0 {
					revisions[name] = rev
				}
			}
		}
		missing := 0
		for caseID := range declared {
			if revisions[caseID] == 0 {
				missing++
			}
		}
		if missing == 0 {
			return revisions, nil
		}
		if time.Now().After(deadline) {
			// The bare count cannot distinguish "the CRs are not in the
			// cluster" from "they are there but the operator never patched
			// them" from "they are patched and the read is wrong". Capture
			// exactly that state before giving up, so the failure carries its
			// own diagnosis.
			return nil, fmt.Errorf("%d declared definitions have no active revision after 10m; diagnosis follows\n%s",
				missing, diagnoseRevisionWait(tenants, declared))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// withBackoff retries fn on rate limiting (429), transport errors, and 5xx --
// the control plane's rate limiter is the expected case while prepare mints
// keys and reads policies through the bootstrap credential.
func withBackoff(ctx context.Context, fn func() error) error {
	delay := 200 * time.Millisecond
	for attempt := 0; ; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		var aerr *APIError
		if !errors.As(err, &aerr) || (aerr.StatusCode != http.StatusTooManyRequests && aerr.StatusCode != 0 && aerr.StatusCode < 500) {
			return err
		}
		if attempt >= 4 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay < 3*time.Second {
			delay *= 2
		}
	}
}

// scheduledJobList is the shape `kubectl get scheduledjobs -o json` returns,
// narrowed to the two fields the revision wait reads.
type scheduledJobList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Status struct {
			ActiveRevision int64 `json:"activeRevision"`
		} `json:"status"`
	} `json:"items"`
}

// scheduledJobRevisions returns name -> activeRevision for every ScheduledJob
// in one namespace. Parse errors are fatal, not skipped: a half-decoded read
// would look exactly like "the operator never projected anything" and send
// the wait into its deadline instead of reporting the real fault.
func scheduledJobRevisions(ctx context.Context, namespace string) (map[string]int64, error) {
	out, err := exec.CommandContext(ctx, "kubectl", "get", "scheduledjobs",
		"-n", namespace, "-o", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("kubectl get scheduledjobs -n %s: %v: %s", namespace, err, strings.TrimSpace(string(out)))
	}
	var list scheduledJobList
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("decode scheduledjobs in %s: %w", namespace, err)
	}
	revisions := make(map[string]int64, len(list.Items))
	for _, item := range list.Items {
		revisions[item.Metadata.Name] = item.Status.ActiveRevision
	}
	return revisions, nil
}

// diagnoseRevisionWait snapshots the cluster state a revision wait depends
// on: do the CRs exist, do they carry a status, does the operator's namespace
// table cover their namespaces. Each command fails soft — the diagnosis is
// evidence, and a failed piece of evidence is itself evidence.
func diagnoseRevisionWait(tenants []string, declared map[string]string) string {
	var b strings.Builder
	run := func(label string, args ...string) {
		out, err := exec.Command("kubectl", args...).CombinedOutput()
		fmt.Fprintf(&b, "--- %s\n", label)
		if err != nil {
			fmt.Fprintf(&b, "(kubectl failed: %v) %s\n", err, strings.TrimSpace(string(out)))
			return
		}
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = "(no output)"
		}
		fmt.Fprintf(&b, "%s\n", text)
	}
	for _, tenantID := range tenants {
		ns := TenantNamespace(tenantID)
		run("namespaces: "+ns, "get", "namespace", ns, "-o", "jsonpath={.metadata.name} phase={.status.phase}")
		run("scheduledjobs in "+ns, "get", "scheduledjobs", "-n", ns,
			"-o", "jsonpath={range .items[*]}{.metadata.name} gen={.metadata.generation} status={.status}{'\\n'}{end}")
		run("crds", "get", "crd", "scheduledjobs.workloads.orbitjob.io",
			"-o", "jsonpath={.metadata.name} established={.status.conditions[?(@.type=='Established')].status}")
	}
	run("operator env OPERATOR_NAMESPACE_TENANTS", "get", "deployment", "orbitjob-operator",
		"-n", WorkloadNamespace(),
		"-o", "jsonpath={range .spec.template.spec.containers[*].env[*]}{.name}={.value}{'\\n'}{end}")
	sample := ""
	for caseID := range declared {
		sample = caseID
		break
	}
	if sample != "" && len(tenants) > 0 {
		run(fmt.Sprintf("sample CR %s", sample), "get", "scheduledjobs", sample,
			"-n", TenantNamespace(tenants[0]), "-o", "yaml")
	}
	return b.String()
}

func kubectlApply(path string) error {
	out, err := exec.Command("kubectl", "apply", "-f", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func kubectlRolloutRestart(target, namespace string) error {
	out, err := exec.Command("kubectl", "rollout", "restart", target, "-n", namespace).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func kubectlWait(target, namespace string, timeout time.Duration) error {
	out, err := exec.Command("kubectl", "wait", target, "-n", namespace, "--for=condition=Available", "--timeout="+timeout.String()).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func loadGeneratedDefinitions(path string) ([]Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var definitions []Definition
	if err := json.Unmarshal(data, &definitions); err != nil {
		return nil, err
	}
	return definitions, nil
}

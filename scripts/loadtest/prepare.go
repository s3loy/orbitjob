package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type kubeObject struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
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

// CreatedDefinition records the mapping from a generated case to its API-created job.
type CreatedDefinition struct {
	CaseID string `json:"case_id"`
	JobID  int64  `json:"job_id"`
	Tenant string `json:"tenant"`
}

// Prepare applies load fixtures, creates tenants with API keys, creates every
// generated definition through the Admin API, and writes the case-to-job mapping
// consumed by the run engine. Tenant keys are written with 0600 permissions.
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

	manifests := []string{"namespace.yaml", "operations-rbac.yaml", "fixture-configmap.yaml", "fixture.yaml", "postgres.yaml"}
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

	tenantKeys := make(map[string]string, len(cfg.Tenants))
	for _, slug := range cfg.Tenants {
		id, err := bootstrap.CreateTenant(ctx, slug, slug)
		if err != nil {
			return fmt.Errorf("create tenant %s: %w", slug, err)
		}
		key, err := bootstrap.CreateAPIKey(ctx, id)
		if err != nil {
			return fmt.Errorf("create api key %s: %w", slug, err)
		}
		tenantKeys[slug] = key
	}
	if err := writeStableJSON(filepath.Join(runDir, "tenant-keys.json"), tenantKeys); err != nil {
		return fmt.Errorf("write tenant keys: %w", err)
	}

	created := make([]CreatedDefinition, 0, len(defs))
	for _, def := range defs {
		key, ok := tenantKeys[def.Tenant]
		if !ok {
			return fmt.Errorf("no api key for tenant %s", def.Tenant)
		}
		client := NewAPIClient(apiURL, key)
		jobID, err := client.CreateJob(ctx, def.Tenant, def.Request)
		if err != nil {
			return fmt.Errorf("create job %s: %w", def.CaseID, err)
		}
		created = append(created, CreatedDefinition{CaseID: def.CaseID, JobID: jobID, Tenant: def.Tenant})
	}
	if err := writeStableJSON(filepath.Join(runDir, "created-definitions.json"), created); err != nil {
		return fmt.Errorf("write created definitions: %w", err)
	}
	fmt.Printf("prepare: created %d definitions across %d tenants\n", len(created), len(tenantKeys))
	return nil
}

func kubectlApply(path string) error {
	out, err := exec.Command("kubectl", "apply", "-f", path).CombinedOutput()
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

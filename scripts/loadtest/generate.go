package main

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type ScenarioFile struct {
	SchemaVersion string           `yaml:"schema_version"`
	Category      string           `yaml:"category"`
	Families      []ScenarioFamily `yaml:"families"`
}

type ScenarioFamily struct {
	ID            string `yaml:"id"`
	Count         int    `yaml:"count"`
	Image         string `yaml:"image"`
	TerminalState string `yaml:"terminal_state"`
}

type Manifest struct {
	Definitions []Definition `json:"definitions"`
}

func LoadScenarios(dir string) ([]ScenarioFile, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var scenarios []ScenarioFile
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var scenario ScenarioFile
		if err := yaml.Unmarshal(data, &scenario); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		sort.Slice(scenario.Families, func(i, j int) bool { return scenario.Families[i].ID < scenario.Families[j].ID })
		scenarios = append(scenarios, scenario)
	}
	return scenarios, nil
}

func Generate(cfg Config, scenarios []ScenarioFile, lock ImageLock) (Manifest, error) {
	images := map[string]LockedImage{}
	for _, image := range lock.Images {
		images[image.Name] = image
	}
	sum := sha256.Sum256([]byte(cfg.Seed))
	rng := rand.New(rand.NewSource(int64(binary.BigEndian.Uint64(sum[:8]))))
	manifest := Manifest{}
	index := 0
	for _, scenario := range scenarios {
		for _, family := range scenario.Families {
			image, ok := images[family.Image]
			if !ok {
				return Manifest{}, fmt.Errorf("family %s references unknown image %s", family.ID, family.Image)
			}
			for i := 1; i <= family.Count; i++ {
				origin := "manual"
				productTrigger := "manual"
				if index < 360 {
					origin, productTrigger = "cron", "cron"
				} else if index < 480 {
					origin = "webhook"
				}
				terminal := family.TerminalState
				if terminal == "" {
					terminal = "success"
				}
				caseID := fmt.Sprintf("%s-%s-%04d", scenario.Category, family.ID, i)
				definition := Definition{
					CaseID: caseID, Tenant: cfg.Tenants[index%len(cfg.Tenants)], Category: scenario.Category,
					ProductTriggerType: productTrigger, TriggerOrigin: origin,
					Request: map[string]any{
						"name": caseID, "trigger_type": productTrigger, "handler_type": "container",
						"handler_payload": map[string]any{
							"image": ImageReference(image), "env": map[string]any{"CASE_ID": caseID},
							"command": workloadCommand(family.ID), "args": workloadArgs(family.ID),
						},
					},
					Expected: Expected{TerminalState: terminal, Attempts: 1, Core: scenario.Category != "external-probes"},
				}
				if productTrigger == "cron" {
					definition.Request["cron_expr"] = fmt.Sprintf("%d * * * *", rng.Intn(60))
					definition.Request["timezone"] = "UTC"
				}
				manifest.Definitions = append(manifest.Definitions, definition)
				index++
			}
		}
	}
	if len(manifest.Definitions) != cfg.Definitions.Total {
		return Manifest{}, fmt.Errorf("generated %d definitions, expected %d", len(manifest.Definitions), cfg.Definitions.Total)
	}
	return manifest, nil
}

func workloadCommand(family string) []any {
	switch {
	case contains(family, "database", "query", "insert", "upsert", "pagination", "transaction", "version", "statement"):
		return []any{"psql"}
	case contains(family, "http", "webhook", "api", "download", "response", "tls"):
		return []any{"curl"}
	case contains(family, "pod", "deployment", "job", "configmap", "endpoint", "rbac", "label"):
		return []any{"kubectl"}
	default:
		return []any{"python", "-c"}
	}
}

func workloadArgs(family string) []any {
	switch workloadCommand(family)[0] {
	case "psql":
		return []any{"-v", "ON_ERROR_STOP=1", "-c", "SELECT count(*) FROM workload_rows"}
	case "curl":
		return []any{"--fail-with-body", "--retry", "2", "${FIXTURE_URL}/api/pages?page=1"}
	case "kubectl":
		return []any{"get", "pods", "-n", "orbitjob"}
	default:
		return []any{"import hashlib,json; data={'case':'${CASE_ID}','values':list(range(128))}; print(hashlib.sha256(json.dumps(data,sort_keys=True).encode()).hexdigest())"}
	}
}

func contains(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if len(candidate) <= len(value) {
			for i := 0; i+len(candidate) <= len(value); i++ {
				if value[i:i+len(candidate)] == candidate {
					return true
				}
			}
		}
	}
	return false
}

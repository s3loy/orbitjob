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

// Fixture and load-Postgres endpoints are stable in-cluster addresses created by
// deploy/load/*.yaml. Database families need PG* env so psql can reach load-postgres.
const (
	fixtureURL     = "http://load-fixture.orbitjob-load.svc:8080"
	loadPGHost     = "load-postgres.orbitjob-load.svc.cluster.local"
	loadPGUser     = "loadtest"
	loadPGPassword = "loadtest-local"
	loadPGDatabase = "loadtest"
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

	triggerAssignments, err := assignTriggers(cfg, rng)
	if err != nil {
		return Manifest{}, fmt.Errorf("assign triggers: %w", err)
	}

	manifest := Manifest{}
	index := 0
	for _, scenario := range scenarios {
		for _, family := range scenario.Families {
			image, ok := images[family.Image]
			if !ok {
				return Manifest{}, fmt.Errorf("family %s references unknown image %s", family.ID, family.Image)
			}
			for i := 1; i <= family.Count; i++ {
				assignment := triggerAssignments[index]
				origin := assignment.Origin
				productTrigger := assignment.ProductTrigger
				terminal := family.TerminalState
				if terminal == "" {
					terminal = "success"
				}
				caseID := fmt.Sprintf("%s-%s-%04d", scenario.Category, family.ID, i)
				env := map[string]any{"CASE_ID": caseID, "FIXTURE_URL": fixtureURL}
				if family.Image == "postgres" {
					env["PGHOST"] = loadPGHost
					env["PGUSER"] = loadPGUser
					env["PGPASSWORD"] = loadPGPassword
					env["PGDATABASE"] = loadPGDatabase
					env["PGCONNECT_TIMEOUT"] = "10"
					env["PGSSLMODE"] = "disable"
				}
				handlerPayload := map[string]any{
					"image":   ImageReference(image),
					"env":     env,
					"command": workloadCommand(family.Image),
					"args":    workloadArgs(family.Image, terminal),
					// Limits must stay within the orbitjob-tasks LimitRange max (500m / 256Mi)
					// or the K8s admission controller forbids pod creation and every job times out.
					"resources": map[string]any{
						"requests": map[string]any{"cpu": "50m", "memory": "32Mi"},
						"limits":   map[string]any{"cpu": "500m", "memory": "256Mi"},
					},
				}
				if family.Image == "kubectl" {
					handlerPayload["service_account_name"] = "orbitjob-load-operations"
					handlerPayload["automount_service_account_token"] = true
				}
				timeoutSec := 60
				switch scenario.Category {
				case "database", "http-webhook", "external-probes":
					timeoutSec = 120
				case "operations":
					timeoutSec = 180
				case "failure":
					timeoutSec = 90
				}
				definition := Definition{
					CaseID: caseID, Tenant: cfg.Tenants[index%len(cfg.Tenants)], Category: scenario.Category,
					ProductTriggerType: productTrigger, TriggerOrigin: origin,
					Request: map[string]any{
						"name": caseID, "trigger_type": productTrigger, "handler_type": "container",
						"timeout_sec":     timeoutSec,
						"handler_payload": handlerPayload,
					},
					Expected: Expected{TerminalState: terminal, Attempts: 1, Core: scenario.Category != "external-probes"},
				}
				if productTrigger == "cron" {
					definition.Request["cron_expr"] = cronExpr(cfg.Definitions.CronIntervalMinutes, rng)
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

type triggerAssignment struct {
	ProductTrigger string
	Origin         string
}

// assignTriggers deterministically distributes product trigger types and trigger
// origins across the definition indices. It respects the counts declared in the
// config and shuffles them using the seeded RNG so the distribution is stable
// but not clustered by scenario family order.
func assignTriggers(cfg Config, rng *rand.Rand) ([]triggerAssignment, error) {
	total := cfg.Definitions.Total
	if total <= 0 {
		return nil, fmt.Errorf("definitions.total must be > 0")
	}
	product := cfg.Definitions.ProductTriggerTypes
	origin := cfg.Definitions.TriggerOrigins

	cronCount := product["cron"]
	manualProductCount := product["manual"]
	webhookOriginCount := origin["webhook"]
	manualOriginCount := origin["manual"]
	cronOriginCount := origin["cron"]

	if cronCount != cronOriginCount {
		return nil, fmt.Errorf("cron product count %d != cron origin count %d", cronCount, cronOriginCount)
	}
	if manualProductCount != manualOriginCount+webhookOriginCount {
		return nil, fmt.Errorf("manual product count %d != manual origin %d + webhook origin %d", manualProductCount, manualOriginCount, webhookOriginCount)
	}
	if cronCount+manualProductCount != total {
		return nil, fmt.Errorf("product trigger counts sum to %d, expected %d", cronCount+manualProductCount, total)
	}
	if manualOriginCount+cronOriginCount+webhookOriginCount != total {
		return nil, fmt.Errorf("trigger origin counts sum to %d, expected %d", manualOriginCount+cronOriginCount+webhookOriginCount, total)
	}

	assignments := make([]triggerAssignment, total)
	idx := 0
	for i := 0; i < cronCount; i++ {
		assignments[idx] = triggerAssignment{ProductTrigger: "cron", Origin: "cron"}
		idx++
	}
	for i := 0; i < webhookOriginCount; i++ {
		assignments[idx] = triggerAssignment{ProductTrigger: "manual", Origin: "webhook"}
		idx++
	}
	for i := 0; i < manualOriginCount; i++ {
		assignments[idx] = triggerAssignment{ProductTrigger: "manual", Origin: "manual"}
		idx++
	}

	rng.Shuffle(total, func(i, j int) {
		assignments[i], assignments[j] = assignments[j], assignments[i]
	})
	return assignments, nil
}

// cronExpr returns a deterministic cron expression that fires every
// intervalMinutes minutes at a random offset within that window.
// intervalMinutes must evenly divide 60.
func cronExpr(intervalMinutes int, rng *rand.Rand) string {
	if intervalMinutes <= 0 {
		intervalMinutes = 60
	}
	if 60%intervalMinutes != 0 {
		intervalMinutes = 60
	}
	offset := rng.Intn(intervalMinutes)
	return fmt.Sprintf("%d-59/%d * * * *", offset, intervalMinutes)
}

// workloadCommand picks the entrypoint by image, not by family ID. The image is
// the source of truth (declared in each scenario family), so the command must
// match what the image actually ships. Routing by family ID strings previously
// sent alpine families into the python branch even though alpine has no python.
func workloadCommand(image string) []any {
	switch image {
	case "python":
		return []any{"python", "-c"}
	case "alpine", "busybox":
		return []any{"/bin/sh", "-c"}
	case "postgres":
		return []any{"psql"}
	case "curl":
		return []any{"curl"}
	case "kubectl":
		return []any{"kubectl"}
	default:
		return []any{"python", "-c"}
	}
}

// workloadArgs returns image-native args. terminalState=="failed" selects a
// script that exits non-zero so the job reaches the failed terminal state
// instead of accidentally succeeding.
func workloadArgs(image, terminalState string) []any {
	failed := terminalState == "failed"
	switch image {
	case "python":
		if failed {
			return []any{"import sys; print('forced failure $(CASE_ID)'); sys.exit(1)"}
		}
		return []any{"import hashlib,json; data={'case':'$(CASE_ID)','values':list(range(128))}; print(hashlib.sha256(json.dumps(data,sort_keys=True).encode()).hexdigest())"}
	case "alpine":
		if failed {
			return []any{"echo 'checksum mismatch $(CASE_ID)'; exit 1"}
		}
		return []any{"echo -n \"$(CASE_ID)\" | sha256sum"}
	case "busybox":
		return []any{"nslookup load-postgres.orbitjob-load.svc.cluster.local"}
	case "postgres":
		if failed {
			return []any{"-v", "ON_ERROR_STOP=1", "-c", "SELECT syntax_error_from_loadtest"}
		}
		return []any{"-v", "ON_ERROR_STOP=1", "-c", "SELECT 1"}
	case "curl":
		if failed {
			return []any{"--fail-with-body", "--retry", "3", "--retry-delay", "2", "--retry-max-time", "30", "--connect-timeout", "5", "--max-time", "30", "$(FIXTURE_URL)/api/fail-then-succeed?case_id=$(CASE_ID)"}
		}
		return []any{"--fail-with-body", "--retry", "3", "--retry-delay", "2", "--retry-max-time", "30", "--connect-timeout", "5", "--max-time", "30", "$(FIXTURE_URL)/api/pages?page=1"}
	case "kubectl":
		return []any{"get", "pods", "-n", "orbitjob"}
	default:
		return []any{"import hashlib,json; print(hashlib.sha256('$(CASE_ID)'.encode()).hexdigest())"}
	}
}

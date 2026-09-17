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
	fixtureTLSURL  = "https://load-fixture.orbitjob-load.svc:8443"
	loadPGHost     = "load-postgres.orbitjob-load.svc.cluster.local"
	loadPGUser     = "loadtest"
	loadPGPassword = "loadtest-local"
	loadPGDatabase = "loadtest"

	// operationsDeniedSecret is the Secret the operations Role is forbidden to
	// read. ValidateLoadManifests rejects any operations Role that grants
	// secrets, so a read of this Secret is denied by RBAC and the job fails.
	operationsDeniedSecret = "orbitjob-database"
)

type ScenarioFile struct {
	Category string           `yaml:"category"`
	Families []ScenarioFamily `yaml:"families"`
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

	// Generated definitions raise retention so history pruning cannot delete
	// evidence while the run is still going. A definition receives at most one
	// event per schedule cycle plus a burst hit, both far below the corpus
	// size, so the corpus size is a ceiling on runs per definition and is
	// derived from the config rather than invented.
	history := cfg.Definitions.Total

	manifest := Manifest{}
	index := 0
	for _, scenario := range scenarios {
		for _, family := range scenario.Families {
			image, ok := images[family.Image]
			if !ok {
				return Manifest{}, fmt.Errorf("family %s references unknown image %s", family.ID, family.Image)
			}
			for i := 1; i <= family.Count; i++ {
				terminal := family.TerminalState
				if terminal == "" {
					terminal = "success"
				}
				// A scenario expecting a cancel cannot be cron-triggered: the
				// scheduler fires those, the load generator never learns their
				// run id, and nothing else cancels them -- so the expectation
				// is unreachable. The assignment is swapped rather than
				// overwritten so the config's trigger distribution still holds.
				if terminal == terminalStateCanceled {
					swapCancelAssignment(triggerAssignments, index)
				}
				assignment := triggerAssignments[index]
				origin := assignment.Origin
				productTrigger := assignment.ProductTrigger
				caseID := fmt.Sprintf("%s-%s-%04d", scenario.Category, family.ID, i)
				timeoutSec := 60
				switch scenario.Category {
				case "database", "http-webhook", "external-probes":
					timeoutSec = 120
				case "operations":
					timeoutSec = 180
				case "failure":
					timeoutSec = 90
				}
				// Manual definitions need a schedule the CRD accepts but the
				// scheduler never fires; cron definitions get the real
				// interval the config declares.
				schedule := neverFiresCron
				if productTrigger == "cron" {
					schedule = cronExpr(cfg.Definitions.CronIntervalMinutes, rng)
				}
				definition := Definition{
					CaseID: caseID, Tenant: cfg.Tenants[index%len(cfg.Tenants)], Category: scenario.Category,
					ProductTriggerType: productTrigger, TriggerOrigin: origin,
					ScheduledJob: ScheduledJobSpec{
						Schedule:          schedule,
						HistorySuccessful: history,
						HistoryFailed:     history,
						TimeoutSeconds:    timeoutSec,
						Image:             ImageReference(image),
						Command:           workloadCommand(family.Image),
						Args:              workloadArgs(family.Image, family.ID, terminal, caseID),
						// The platform owns retry: a run's attempts are the
						// operator's decision, so the pod-level restart policy
						// stays single-shot and backoffLimit zero.
						BackoffLimit: 0,
					},
					Expected: Expected{TerminalState: terminal, Attempts: 1, Core: scenario.Category != "external-probes"},
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
func workloadCommand(image string) []string {
	switch image {
	case "python":
		return []string{"python", "-c"}
	case "alpine", "busybox":
		return []string{"/bin/sh", "-c"}
	case "postgres":
		return []string{"psql"}
	case "curl":
		return []string{"curl"}
	case "kubectl":
		return []string{"kubectl"}
	default:
		return []string{"python", "-c"}
	}
}

// workloadArgs returns image-native args. terminalState=="failed" selects a
// script that exits non-zero so the job reaches the failed terminal state
// instead of accidentally succeeding.
//
// The operator renders Kubernetes Jobs with image, command and args only --
// there is no env block and no service account field -- so every value a body
// needs (its case id, the fixture addresses, the database DSN) is baked into
// the arguments as a literal. Each definition gets its own args, which is what
// makes the case id available to a body at all.
//
// The entrypoint is routed by image and the arguments by family. That split is
// deliberate: an image only offers one way to start, while two families that
// share an image do entirely different work. Routing arguments by image alone
// sent every success-expecting curl family at the same plain 200 page, so the
// families named for retry, rate limiting, timeouts and content negotiation
// passed without ever meeting the condition in their name, and the fixture
// ledger -- the only input the duplicate-side-effect check has -- stayed empty.
func workloadArgs(image, familyID, terminalState, caseID string) []string {
	failed := terminalState == "failed"
	switch image {
	case "python":
		if failed {
			return []string{fmt.Sprintf("import sys; print('forced failure %s'); sys.exit(1)", caseID)}
		}
		if terminalState == terminalStateCanceled {
			// A scenario that expects a cancel has to still be running when the
			// cancel arrives. The ordinary body finishes in three to six
			// seconds, which made the outcome a race the generator won only
			// when it was quick. The job timeout is 90s, so a cancel that never
			// comes still ends this rather than hanging forever.
			return []string{fmt.Sprintf("import time; print('awaiting cancel %s'); time.sleep(60)", caseID)}
		}
		return []string{fmt.Sprintf("import hashlib,json; data={'case':'%s','values':list(range(128))}; print(hashlib.sha256(json.dumps(data,sort_keys=True).encode()).hexdigest())", caseID)}
	case "alpine":
		if failed {
			return []string{fmt.Sprintf("echo 'checksum mismatch %s'; exit 1", caseID)}
		}
		return []string{fmt.Sprintf("echo -n '%s' | sha256sum", caseID)}
	case "busybox":
		return []string{"nslookup load-postgres.orbitjob-load.svc.cluster.local"}
	case "postgres":
		// The connection rides the first argument as a URI: no PG* environment
		// exists on an operator-rendered Job.
		uri := fmt.Sprintf("postgresql://%s:%s@%s:5432/%s?connect_timeout=10&sslmode=disable",
			loadPGUser, loadPGPassword, loadPGHost, loadPGDatabase)
		if failed {
			return []string{uri, "-v", "ON_ERROR_STOP=1", "-c", "SELECT syntax_error_from_loadtest"}
		}
		return []string{uri, "-v", "ON_ERROR_STOP=1", "-c", "SELECT 1"}
	case "curl":
		return curlArgs(familyID, failed, caseID)
	case "kubectl":
		// Operator-rendered Jobs run as the namespace's default service
		// account, which holds no grants. A read that needs any permission is
		// therefore denied by the API server and exits non-zero, which is what
		// this failure-expecting family asserts.
		return []string{"get", "secret", operationsDeniedSecret, "-n", WorkloadNamespace()}
	default:
		return []string{fmt.Sprintf("import hashlib,json; print(hashlib.sha256('%s'.encode()).hexdigest())", caseID)}
	}
}

// curlArgs picks the fixture endpoint and the curl flags for one scenario
// family. A family that expects success has an endpoint that makes it succeed
// for the reason its name gives, and a family that expects failure fails the
// way it is named to as well.
func curlArgs(familyID string, failed bool, caseID string) []string {
	failOnError := []string{"--fail-with-body", "--connect-timeout", "5"}
	retryTransient := []string{"--retry", "3", "--retry-delay", "2", "--retry-max-time", "30"}
	// post builds a request whose idempotency key is the case id, so a retried
	// attempt of the same definition presents the same key and the fixture can
	// tell a duplicate delivery from a new one.
	post := func(path, contentType, body string) []string {
		return []string{"-H", "Content-Type: " + contentType, "--data", body,
			"--max-time", "30", fixtureURL + path}
	}

	switch familyID {
	case "rate-limit-then-success":
		// 429 twice, then 200. curl treats 429 as transient, so it is the
		// retries that carry the job to its success.
		return flatArgs(failOnError, retryTransient, []string{
			"--max-time", "30", fixtureURL + "/api/rate-limit-then-success?case_id=" + caseID})
	case "server-error-then-success", "retry-until-success":
		// 500 twice, then 200. --retry 3 allows four attempts, so the third
		// one lands and the job succeeds.
		return flatArgs(failOnError, retryTransient, []string{
			"--max-time", "30", fixtureURL + "/api/fail-then-succeed?case_id=" + caseID})
	case "retry-exhausted":
		// Never succeeds, so all four attempts are spent and curl exits 22.
		return flatArgs(failOnError, retryTransient, []string{
			"--max-time", "30", fixtureURL + "/api/always-fail?case_id=" + caseID})
	case "slow-response-timeout":
		// The fixture holds the response for 30s and the client gives up at 5,
		// so the failure is the client's own deadline and not an error status.
		return flatArgs(failOnError, []string{
			"--max-time", "5", fixtureURL + "/api/slow?case_id=" + caseID})
	case "invalid-json-content-type":
		// A body that does not claim to be JSON; the endpoint answers 415.
		return flatArgs(failOnError, post("/api/require-json", "text/plain", fmt.Sprintf(`{"case":"%s"}`, caseID)))
	case "webhook-delivery":
		return flatArgs(failOnError, post("/webhook-origin", "application/json",
			fmt.Sprintf(`{"case_id":"%s","operation":"delivery","idempotency_key":"%s"}`, caseID, caseID)))
	case "idempotent-webhook":
		// The same delivery twice in one invocation, keyed the same both times.
		// One exit code covers both transfers, so a second delivery that is not
		// recognised as a duplicate fails the job.
		delivery := []string{"-H", "Content-Type: application/json",
			"--data", fmt.Sprintf(`{"case_id":"%s","operation":"idempotent","idempotency_key":"%s"}`, caseID, caseID),
			"--max-time", "30", fixtureURL + "/ledger"}
		return flatArgs(failOnError, delivery, []string{"--next"}, delivery)
	case "file-download", "fixture-json-download":
		return flatArgs(failOnError, []string{
			"--max-time", "30", fixtureURL + "/fixtures/json/events.json"})
	case "tls-handshake":
		return flatArgs(failOnError, []string{
			"--insecure", "--max-time", "30", fixtureTLSURL + "/healthz"})
	case "tls-json-body":
		return flatArgs(failOnError, []string{
			"--insecure", "--max-time", "30", fixtureTLSURL + "/api/pages?page=1"})
	}
	if failed {
		// A curl family that declares a failure but has no route of its own
		// still has to fail; without this a new family would quietly succeed.
		return flatArgs(failOnError, retryTransient, []string{
			"--max-time", "30", fixtureURL + "/api/always-fail?case_id=" + caseID})
	}
	return flatArgs(failOnError, []string{
		"--max-time", "30", fixtureURL + "/api/pages?page=1"})
}

// flatArgs flattens argument fragments into one argument list.
func flatArgs(parts ...[]string) []string {
	var out []string
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

// swapCancelAssignment gives the definition at index a manual trigger when it
// holds a cron one, exchanging with the next manual slot so the total counts of
// each trigger type are unchanged.
func swapCancelAssignment(assignments []triggerAssignment, index int) {
	if assignments[index].ProductTrigger != "cron" {
		return
	}
	for j := index + 1; j < len(assignments); j++ {
		if assignments[j].ProductTrigger == "manual" && assignments[j].Origin == "manual" {
			assignments[index], assignments[j] = assignments[j], assignments[index]
			return
		}
	}
}

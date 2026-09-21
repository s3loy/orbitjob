package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestGenerateStandardManifest(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/standard.yaml")
	if err != nil {
		t.Fatal(err)
	}
	scenarios, err := LoadScenarios("../../test/load/scenarios")
	if err != nil {
		t.Fatal(err)
	}
	images, err := LoadImageLock("../../test/load/config/images.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	a, err := Generate(cfg, scenarios, images)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(cfg, scenarios, images)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Definitions) != 1200 || !reflect.DeepEqual(a, b) {
		t.Fatalf("manifest size=%d deterministic=%v", len(a.Definitions), reflect.DeepEqual(a, b))
	}
	counts := map[string]int{}
	for _, definition := range a.Definitions {
		counts[definition.Category]++
	}
	for category, want := range cfg.Definitions.Categories {
		if counts[category] != want {
			t.Errorf("%s=%d want=%d", category, counts[category], want)
		}
	}
}

func TestWebhookOriginUsesManualProductTrigger(t *testing.T) {
	manifest := mustGenerate(t)
	count := 0
	for _, definition := range manifest.Definitions {
		if definition.TriggerOrigin != "webhook" {
			continue
		}
		count++
		if definition.ProductTriggerType != "manual" {
			t.Fatalf("%s has unsupported product trigger", definition.CaseID)
		}
	}
	if count != 120 {
		t.Fatalf("webhook-origin definitions=%d", count)
	}
}

func TestTriggerTypeCountsMatchConfig(t *testing.T) {
	manifest := mustGenerate(t)
	cfg, _ := LoadConfig("../../test/load/config/standard.yaml")
	counts := map[string]int{"manual": 0, "cron": 0}
	origins := map[string]int{"manual": 0, "cron": 0, "webhook": 0}
	for _, def := range manifest.Definitions {
		counts[def.ProductTriggerType]++
		origins[def.TriggerOrigin]++
	}
	for _, tt := range []struct {
		name string
		got  map[string]int
		want map[string]int
	}{
		{"product_trigger_types", counts, cfg.Definitions.ProductTriggerTypes},
		{"trigger_origins", origins, cfg.Definitions.TriggerOrigins},
	} {
		for k, want := range tt.want {
			if tt.got[k] != want {
				t.Fatalf("%s %s=%d, want %d", tt.name, k, tt.got[k], want)
			}
		}
	}
}

// Manual definitions satisfy the CRD's non-empty schedule with an expression
// that matches no instant, so the scheduler never fires occurrences the
// generator did not ask for.
func TestManualDefinitionsUseANeverFiringSchedule(t *testing.T) {
	manifest := mustGenerate(t)
	for _, def := range manifest.Definitions {
		if def.ProductTriggerType != "manual" {
			continue
		}
		if def.ScheduledJob.Schedule != neverFiresCron {
			t.Fatalf("%s: manual definition schedule %q, want %q", def.CaseID, def.ScheduledJob.Schedule, neverFiresCron)
		}
	}
}

func TestCronExpressionsUseConfigInterval(t *testing.T) {
	manifest := mustGenerate(t)
	cfg, _ := LoadConfig("../../test/load/config/standard.yaml")
	interval := cfg.Definitions.CronIntervalMinutes
	if interval <= 0 {
		interval = 60
	}
	for _, def := range manifest.Definitions {
		if def.ProductTriggerType != "cron" {
			continue
		}
		expr := def.ScheduledJob.Schedule
		if expr == "" {
			t.Fatalf("%s: missing schedule", def.CaseID)
		}
		if !strings.Contains(expr, fmt.Sprintf("/%d", interval)) {
			t.Fatalf("%s: schedule %q does not use interval /%d", def.CaseID, expr, interval)
		}
	}
}

// Default retention keeps three runs per outcome, which would delete evidence
// from a run that triggers a definition many times. Generated definitions
// raise the limits to the corpus size, a config-derived ceiling no definition
// can exceed.
func TestGeneratedDefinitionsOutliveRetention(t *testing.T) {
	manifest := mustGenerate(t)
	cfg, _ := LoadConfig("../../test/load/config/standard.yaml")
	for _, def := range manifest.Definitions {
		if def.ScheduledJob.HistorySuccessful < cfg.Definitions.Total {
			t.Fatalf("%s: history.successfulRuns=%d, want >= %d", def.CaseID, def.ScheduledJob.HistorySuccessful, cfg.Definitions.Total)
		}
		if def.ScheduledJob.HistoryFailed < cfg.Definitions.Total {
			t.Fatalf("%s: history.failedRuns=%d, want >= %d", def.CaseID, def.ScheduledJob.HistoryFailed, cfg.Definitions.Total)
		}
	}
}

func mustGenerate(t *testing.T) Manifest {
	t.Helper()
	cfg, _ := LoadConfig("../../test/load/config/standard.yaml")
	scenarios, _ := LoadScenarios("../../test/load/scenarios")
	images, _ := LoadImageLock("../../test/load/config/images.lock.yaml")
	manifest, err := Generate(cfg, scenarios, images)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestWorkloadCommandByImage(t *testing.T) {
	cases := map[string][]string{
		"python":   {"python", "-c"},
		"alpine":   {"/bin/sh", "-c"},
		"busybox":  {"/bin/sh", "-c"},
		"postgres": {"psql"},
		"curl":     {"curl"},
		"kubectl":  {"kubectl"},
	}
	for image, want := range cases {
		got := workloadCommand(image)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("workloadCommand(%q) = %v, want %v", image, got, want)
		}
	}
}

// The operator renders Jobs with image, command and args only, so no generated
// spec may rely on an env block or a service account.
func TestGeneratedSpecsCarryOnlyWhatTheOperatorRenders(t *testing.T) {
	manifest := mustGenerate(t)
	if len(manifest.Definitions) == 0 {
		t.Fatal("no definitions")
	}
	for _, def := range manifest.Definitions {
		if def.ScheduledJob.Image == "" {
			t.Fatalf("%s: image missing", def.CaseID)
		}
		if def.ScheduledJob.TimeoutSeconds <= 0 {
			t.Fatalf("%s: timeoutSeconds=%d, want > 0", def.CaseID, def.ScheduledJob.TimeoutSeconds)
		}
	}
}

func TestAlpineFamilyUsesShNotPython(t *testing.T) {
	manifest := mustGenerate(t)
	for _, def := range manifest.Definitions {
		if !strings.Contains(def.ScheduledJob.Image, "/alpine") {
			continue
		}
		command := def.ScheduledJob.Command
		if len(command) == 0 || command[0] != "/bin/sh" {
			t.Fatalf("%s: alpine image uses command %v, want /bin/sh (alpine has no python)", def.CaseID, command)
		}
	}
}

// Baked-in values are the only configuration a body gets: every curl family
// must carry the fixture address as a literal in its args.
func TestCurlFamilyArgsCarryTheFixtureURL(t *testing.T) {
	manifest := mustGenerate(t)
	sawCurl := false
	for _, def := range manifest.Definitions {
		if !strings.Contains(def.ScheduledJob.Image, "curlimages/curl") {
			continue
		}
		sawCurl = true
		joined := strings.Join(def.ScheduledJob.Args, " ")
		if !strings.Contains(joined, fixtureURL) && !strings.Contains(joined, fixtureTLSURL) {
			t.Fatalf("%s: curl args name no fixture endpoint: %q", def.CaseID, joined)
		}
	}
	if !sawCurl {
		t.Fatal("no curl-family definitions found")
	}
}

func TestFailureFamilyArgsExitNonZero(t *testing.T) {
	manifest := mustGenerate(t)
	sawFailed := false
	for _, def := range manifest.Definitions {
		if def.Expected.TerminalState != "failed" {
			continue
		}
		sawFailed = true
		image := def.ScheduledJob.Image
		joined := strings.ToLower(strings.Join(def.ScheduledJob.Args, " "))
		var signalsFailure bool
		switch {
		case strings.Contains(image, "/python"):
			signalsFailure = strings.Contains(joined, "sys.exit(1)") || strings.Contains(joined, "exit(1)")
		case strings.Contains(image, "/alpine"), strings.Contains(image, "/busybox"):
			signalsFailure = strings.Contains(joined, "exit 1")
		case strings.Contains(image, "/postgres"):
			signalsFailure = strings.Contains(joined, "on_error_stop=1")
		case strings.Contains(image, "kubectl"):
			// The only failing kubectl body is the RBAC denial: the default
			// identity holds no grants, so the API server denies the read.
			signalsFailure = strings.Contains(joined, "secret")
		case strings.Contains(image, "curlimages/curl"):
			signalsFailure = strings.Contains(joined, "fail-then-succeed") || strings.Contains(joined, "fail-with-body")
		}
		if !signalsFailure {
			t.Errorf("%s: expected-failed family args do not signal failure: %v", def.CaseID, def.ScheduledJob.Args)
		}
	}
	if !sawFailed {
		t.Fatal("no expected-failed definitions found")
	}
}

func TestBusyboxTargetsLoadPostgres(t *testing.T) {
	manifest := mustGenerate(t)
	sawBusybox := false
	for _, def := range manifest.Definitions {
		if !strings.Contains(def.ScheduledJob.Image, "/busybox") {
			continue
		}
		if strings.Contains(def.CaseID, "service-account-token-absent") {
			continue
		}
		sawBusybox = true
		joined := strings.Join(def.ScheduledJob.Args, " ")
		if !strings.Contains(joined, "load-postgres.orbitjob-load.svc.cluster.local") {
			t.Errorf("%s: busybox does not target load-postgres: %s", def.CaseID, joined)
		}
	}
	if !sawBusybox {
		t.Fatal("no busybox definitions found")
	}
}

func TestServiceAccountTokenScenarioChecksProjectedTokenAbsence(t *testing.T) {
	manifest := mustGenerate(t)
	found := 0
	for _, def := range manifest.Definitions {
		if !strings.Contains(def.CaseID, "service-account-token-absent") {
			continue
		}
		found++
		if def.Expected.TerminalState != "success" {
			t.Fatalf("%s terminal state = %q, want success", def.CaseID, def.Expected.TerminalState)
		}
		got := strings.Join(def.ScheduledJob.Args, " ")
		want := "test ! -e /var/run/secrets/kubernetes.io/serviceaccount/token"
		if got != want {
			t.Fatalf("%s args = %q, want %q", def.CaseID, got, want)
		}
	}
	if found != 15 {
		t.Fatalf("token absence definitions = %d, want 15", found)
	}
}

func TestCurlArgsHaveRetryAndTimeouts(t *testing.T) {
	manifest := mustGenerate(t)
	sawCurl := false
	for _, def := range manifest.Definitions {
		if !strings.Contains(def.ScheduledJob.Image, "curlimages/curl") {
			continue
		}
		sawCurl = true
		joined := strings.ToLower(strings.Join(def.ScheduledJob.Args, " "))
		// Both bounds are required of every family: without them a hung fixture
		// holds the job until the platform timeout, which is a different
		// failure from the one the scenario declares.
		for _, want := range []string{"--connect-timeout", "--max-time"} {
			if !strings.Contains(joined, want) {
				t.Errorf("%s: curl args missing %s: %v", def.CaseID, want, def.ScheduledJob.Args)
			}
		}
		// A retry delay only makes sense next to a retry.
		if strings.Contains(joined, "--retry ") && !strings.Contains(joined, "--retry-delay") {
			t.Errorf("%s: curl retries with no delay between attempts: %v", def.CaseID, def.ScheduledJob.Args)
		}
		if strings.Contains(joined, "--retry 2") {
			t.Errorf("%s: curl still uses --retry 2", def.CaseID)
		}
	}
	if !sawCurl {
		t.Fatal("no curl-family definitions found")
	}
}

// There is no PG* environment on an operator-rendered Job, so the connection
// rides the first psql argument as a URI -- with its own timeouts and sslmode.
func TestPostgresConnectsByURIFromArgs(t *testing.T) {
	manifest := mustGenerate(t)
	sawPostgres := false
	for _, def := range manifest.Definitions {
		if !strings.Contains(def.ScheduledJob.Image, "/postgres") {
			continue
		}
		sawPostgres = true
		joined := strings.Join(def.ScheduledJob.Args, " ")
		if !strings.Contains(joined, "postgresql://") {
			t.Errorf("%s: psql args carry no connection URI: %s", def.CaseID, joined)
		}
		if !strings.Contains(joined, "connect_timeout=10") || !strings.Contains(joined, "sslmode=disable") {
			t.Errorf("%s: psql URI missing connect timeout or sslmode: %s", def.CaseID, joined)
		}
	}
	if !sawPostgres {
		t.Fatal("no postgres definitions found")
	}
}

func TestCategoryTimeout(t *testing.T) {
	manifest := mustGenerate(t)
	want := map[string]int{
		"data-processing": 60,
		"database":        120,
		"http-webhook":    120,
		"operations":      180,
		"failure":         90,
		"external-probes": 120,
	}
	for _, def := range manifest.Definitions {
		if got := def.ScheduledJob.TimeoutSeconds; got != want[def.Category] {
			t.Errorf("%s: timeoutSeconds=%d, want %d", def.CaseID, got, want[def.Category])
		}
	}
}

// The scripting images carry their case id in the body itself: there is no
// $(VAR) environment for an operator-rendered Job to expand.
func TestWorkloadArgsBakeTheCaseIDIn(t *testing.T) {
	for _, image := range []string{"python", "alpine", "unknown"} {
		args := workloadArgs(image, "sha256-json-document", "success", "data-processing-sha256-json-document-0001")
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "data-processing-sha256-json-document-0001") {
			t.Errorf("workloadArgs(%q) has no case id: %s", image, joined)
		}
	}
}

// No generated arg may contain ${...}: Kubernetes expands $(VAR) in args but
// never ${VAR}, and a literal reaches the process and breaks the URL.
func TestWorkloadArgsUseNoShellExpansionSyntax(t *testing.T) {
	for _, image := range []string{"python", "alpine", "busybox", "postgres", "curl", "kubectl", "unknown"} {
		for _, terminal := range []string{"success", "failed"} {
			for _, arg := range workloadArgs(image, "paginated-api", terminal, "http-webhook-paginated-api-0001") {
				if strings.Contains(arg, "${") {
					t.Errorf("workloadArgs(%q, %q) contains ${...} literal: %s", image, terminal, arg)
				}
			}
		}
	}
}

// A cancel scenario has to still be running when the cancel arrives. The
// ordinary python body finishes in three to six seconds, which left the outcome
// to a race the generator only sometimes won.
func TestWorkloadArgsSleepForCancelScenarios(t *testing.T) {
	args := workloadArgs("python", "cancel-while-running", terminalStateCanceled, "failure-cancel-while-running-0001")
	if len(args) == 0 {
		t.Fatal("no args for a cancel scenario")
	}
	if !strings.Contains(args[0], "sleep") {
		t.Fatalf("cancel scenario would finish before the cancel lands: %s", args[0])
	}
}

// The ordinary body must stay fast; a slow default would change every other
// scenario's timing.
func TestWorkloadArgsStayFastForSuccessScenarios(t *testing.T) {
	args := workloadArgs("python", "sha256-json-document", "success", "data-processing-sha256-json-document-0001")
	if strings.Contains(args[0], "sleep") {
		t.Fatalf("success scenario should not sleep: %s", args[0])
	}
}

// A cron-triggered cancel scenario is unreachable: the scheduler fires it, the
// generator never sees the run id, and nothing cancels it. The assignment is
// swapped so the trigger distribution keeps its declared counts.
func TestSwapCancelAssignmentMovesCronToManual(t *testing.T) {
	assignments := []triggerAssignment{
		{ProductTrigger: "cron", Origin: "cron"},
		{ProductTrigger: "manual", Origin: "manual"},
	}
	swapCancelAssignment(assignments, 0)
	if assignments[0].ProductTrigger != "manual" || assignments[0].Origin != "manual" {
		t.Fatalf("index 0 still not manual: %+v", assignments[0])
	}
	if assignments[1].ProductTrigger != "cron" || assignments[1].Origin != "cron" {
		t.Fatalf("the swapped partner did not take the cron slot: %+v", assignments[1])
	}
}

func TestSwapCancelAssignmentLeavesManualAlone(t *testing.T) {
	assignments := []triggerAssignment{{ProductTrigger: "manual", Origin: "manual"}}
	swapCancelAssignment(assignments, 0)
	if assignments[0].ProductTrigger != "manual" {
		t.Fatalf("a manual assignment was disturbed: %+v", assignments[0])
	}
}

// curlFamilies is the fixture endpoint each curl family has to reach. Routing
// arguments by image alone collapsed every success-expecting family onto the
// same plain 200 page, so families named for retry, rate limiting, a timeout
// and content negotiation all passed without meeting the condition in their
// name. This table is what stops that from coming back.
var curlFamilies = map[string]string{
	"paginated-api":             "/api/pages?page=1",
	"webhook-delivery":          "/webhook-origin",
	"idempotent-webhook":        "/ledger",
	"file-download":             "/fixtures/json/events.json",
	"fixture-json-download":     "/fixtures/json/events.json",
	"rate-limit-then-success":   "/api/rate-limit-then-success",
	"server-error-then-success": "/api/fail-then-succeed",
	"retry-until-success":       "/api/fail-then-succeed",
	"retry-exhausted":           "/api/always-fail",
	"slow-response-timeout":     "/api/slow",
	"invalid-json-content-type": "/api/require-json",
	"tls-handshake":             fixtureTLSURL + "/healthz",
	"tls-json-body":             fixtureTLSURL + "/api/pages?page=1",
}

// Every curl family the scenario files declare must reach the endpoint its
// name promises, and no endpoint may be declared for a family that no longer
// exists.
func TestCurlFamiliesReachTheirOwnEndpoint(t *testing.T) {
	scenarios, err := LoadScenarios("../../test/load/scenarios")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, scenario := range scenarios {
		for _, family := range scenario.Families {
			if family.Image != "curl" {
				continue
			}
			seen[family.ID] = true
			endpoint, ok := curlFamilies[family.ID]
			if !ok {
				t.Errorf("curl family %s reaches no declared endpoint; it would run the generic page fetch and pass without exercising its name", family.ID)
				continue
			}
			terminal := family.TerminalState
			if terminal == "" {
				terminal = "success"
			}
			joined := strings.Join(workloadArgs("curl", family.ID, terminal, "curl-case-0001"), " ")
			if !strings.Contains(joined, endpoint) {
				t.Errorf("curl family %s does not reach %s: %s", family.ID, endpoint, joined)
			}
		}
	}
	for id := range curlFamilies {
		if !seen[id] {
			t.Errorf("endpoint declared for %s, which no scenario file declares", id)
		}
	}
}

// nonCurlFamilies pins the image and the terminal state of every family that is
// not routed by curlArgs. The curl families are distinguished by the endpoint
// they reach and have their own table; the rest are distinguished only by the
// script their image runs, which is what this table stops from drifting.
//
// Keyed by category/family, not family id alone: invalid-sql appears in both
// database and failure, and a single-id key would resolve it arbitrarily. That
// gap -- a family whose name promised something its body never did -- is what
// let a family expect success while its body could only fail.
var nonCurlFamilies = map[string]struct {
	Image    string
	Terminal string
}{
	"data-processing/sha256-json-document":    {Image: "python", Terminal: "success"},
	"data-processing/sha256-text":             {Image: "alpine", Terminal: "success"},
	"database/select-one":                     {Image: "postgres", Terminal: "success"},
	"database/invalid-sql":                    {Image: "postgres", Terminal: "failed"},
	"failure/forced-exit-nonzero":             {Image: "python", Terminal: "failed"},
	"failure/cancel-while-running":            {Image: "python", Terminal: "canceled"},
	"failure/plain-success":                   {Image: "python", Terminal: "success"},
	"failure/exit-nonzero-message":            {Image: "alpine", Terminal: "failed"},
	"failure/invalid-sql":                     {Image: "postgres", Terminal: "failed"},
	"operations/dns-resolution":               {Image: "busybox", Terminal: "success"},
	"operations/service-account-token-absent": {Image: "busybox", Terminal: "success"},
	"external-probes/dns-lookup":              {Image: "busybox", Terminal: "success"},
}

// Every non-curl family the scenario files declare must match the image and the
// terminal state its body produces, and no body may be declared for a family
// that does not exist. Without this, a family can be added with a name that
// means nothing and no test notices.
func TestNonCurlFamiliesMatchTheirBody(t *testing.T) {
	scenarios, err := LoadScenarios("../../test/load/scenarios")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, scenario := range scenarios {
		for _, family := range scenario.Families {
			if family.Image == "curl" {
				continue
			}
			key := scenario.Category + "/" + family.ID
			seen[key] = true
			want, ok := nonCurlFamilies[key]
			if !ok {
				t.Errorf("non-curl family %s declares no body; its name promises work the image does not run", key)
				continue
			}
			if family.Image != want.Image {
				t.Errorf("%s runs image %s, table says %s", key, family.Image, want.Image)
			}
			terminal := family.TerminalState
			if terminal == "" {
				terminal = "success"
			}
			if terminal != want.Terminal {
				t.Errorf("%s declares terminal_state %s, table says %s", key, terminal, want.Terminal)
			}
		}
	}
	for key := range nonCurlFamilies {
		if !seen[key] {
			t.Errorf("body declared for %s, which no scenario file declares", key)
		}
	}
}

// A family named for a retry has to carry the retry flags, or it passes on its
// first attempt and proves nothing about retrying.
func TestRetryingCurlFamiliesCarryRetryFlags(t *testing.T) {
	for _, family := range []string{
		"rate-limit-then-success", "server-error-then-success",
		"retry-until-success", "retry-exhausted",
	} {
		joined := strings.Join(workloadArgs("curl", family, "success", "curl-case-0001"), " ")
		if !strings.Contains(joined, "--retry") {
			t.Errorf("curl family %s does not retry: %s", family, joined)
		}
	}
}

// The duplicate-side-effect check reads the fixture ledger, so a family has to
// write to it. An empty ledger made that check pass without having looked at
// anything.
func TestWebhookFamiliesWriteToTheLedger(t *testing.T) {
	for _, family := range []string{"webhook-delivery", "idempotent-webhook"} {
		joined := strings.Join(workloadArgs("curl", family, "success", "curl-case-0001"), " ")
		if !strings.Contains(joined, "--data") || !strings.Contains(joined, "idempotency_key") {
			t.Errorf("curl family %s does not post an idempotency key: %s", family, joined)
		}
	}
}

// scheduledJobManifests is what prepare applies; the objects must land in the
// tenant's namespace and carry the declared spec unchanged.
func TestScheduledJobManifestsLandInTheTenantNamespace(t *testing.T) {
	defs := []Definition{{
		CaseID: "data-processing-sha256-json-document-0001",
		Tenant: "20000000000000000000000001",
		ScheduledJob: ScheduledJobSpec{
			Schedule: neverFiresCron, HistorySuccessful: 10, HistoryFailed: 10,
			TimeoutSeconds: 60, Image: "python:3", Command: []string{"python", "-c"},
			Args: []string{"print(1)"}, BackoffLimit: 0,
		},
	}}
	objects := scheduledJobManifests(defs, "20000000000000000000000001")
	if len(objects) != 1 {
		t.Fatalf("objects = %d, want 1", len(objects))
	}
	object := objects[0]
	if object.Kind != "ScheduledJob" {
		t.Fatalf("kind = %q", object.Kind)
	}
	if object.Metadata.Namespace != TenantNamespace("20000000000000000000000001") {
		t.Fatalf("namespace = %q", object.Metadata.Namespace)
	}
	if object.Metadata.Name != defs[0].CaseID {
		t.Fatalf("name = %q, want the case id", object.Metadata.Name)
	}
	schedule, _ := object.Spec["schedule"].(string)
	if schedule != neverFiresCron {
		t.Fatalf("spec.schedule = %q", schedule)
	}
	template, _ := object.Spec["jobTemplate"].(map[string]any)
	if template["image"] != "python:3" {
		t.Fatalf("jobTemplate.image = %v", template["image"])
	}
}

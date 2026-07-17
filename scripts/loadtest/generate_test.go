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
		if definition.ProductTriggerType != "manual" || definition.Request["trigger_type"] != "manual" {
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
		expr, _ := def.Request["cron_expr"].(string)
		if expr == "" {
			t.Fatalf("%s: missing cron_expr", def.CaseID)
		}
		if !strings.Contains(expr, fmt.Sprintf("/%d", interval)) {
			t.Fatalf("%s: cron_expr %q does not use interval /%d", def.CaseID, expr, interval)
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
	cases := map[string][]any{
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

func TestGeneratedHandlerPayloadHasResources(t *testing.T) {
	manifest := mustGenerate(t)
	if len(manifest.Definitions) == 0 {
		t.Fatal("no definitions")
	}
	for _, def := range manifest.Definitions {
		payload, ok := def.Request["handler_payload"].(map[string]any)
		if !ok {
			t.Fatalf("%s: handler_payload missing", def.CaseID)
		}
		resources, ok := payload["resources"].(map[string]any)
		if !ok {
			t.Fatalf("%s: resources missing in handler_payload", def.CaseID)
		}
		limits, ok := resources["limits"].(map[string]any)
		if !ok {
			t.Fatalf("%s: resources.limits missing", def.CaseID)
		}
		if cpu := fmt.Sprint(limits["cpu"]); cpu != "500m" {
			t.Errorf("%s: limits.cpu=%s want 500m (LimitRange max)", def.CaseID, cpu)
		}
		if mem := fmt.Sprint(limits["memory"]); mem != "256Mi" {
			t.Errorf("%s: limits.memory=%s want 256Mi (LimitRange max)", def.CaseID, mem)
		}
	}
}

func TestAlpineFamilyUsesShNotPython(t *testing.T) {
	manifest := mustGenerate(t)
	for _, def := range manifest.Definitions {
		payload, _ := def.Request["handler_payload"].(map[string]any)
		image, _ := payload["image"].(string)
		if !strings.Contains(image, "/alpine") {
			continue
		}
		command, _ := payload["command"].([]any)
		if len(command) == 0 || command[0] != "/bin/sh" {
			t.Fatalf("%s: alpine image uses command %v, want /bin/sh (alpine has no python)", def.CaseID, command)
		}
	}
}

func TestCurlFamilyHasFixtureURL(t *testing.T) {
	manifest := mustGenerate(t)
	sawCurl := false
	for _, def := range manifest.Definitions {
		payload, _ := def.Request["handler_payload"].(map[string]any)
		image, _ := payload["image"].(string)
		if !strings.Contains(image, "curlimages/curl") {
			continue
		}
		sawCurl = true
		env, _ := payload["env"].(map[string]any)
		if env["FIXTURE_URL"] == nil {
			t.Fatalf("%s: curl family missing FIXTURE_URL env", def.CaseID)
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
		payload, _ := def.Request["handler_payload"].(map[string]any)
		image, _ := payload["image"].(string)
		args, _ := payload["args"].([]any)
		joined := strings.ToLower(strings.Join(toStrings(args), " "))
		var signalsFailure bool
		switch {
		case strings.Contains(image, "/python"):
			signalsFailure = strings.Contains(joined, "sys.exit(1)") || strings.Contains(joined, "exit(1)")
		case strings.Contains(image, "/alpine"), strings.Contains(image, "/busybox"):
			signalsFailure = strings.Contains(joined, "exit 1")
		case strings.Contains(image, "/postgres"):
			signalsFailure = strings.Contains(joined, "on_error_stop=1")
		case strings.Contains(image, "curlimages/curl"):
			signalsFailure = strings.Contains(joined, "fail-then-succeed") || strings.Contains(joined, "fail-with-body")
		}
		if !signalsFailure {
			t.Errorf("%s: expected-failed family args do not signal failure: %v", def.CaseID, args)
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
		payload, _ := def.Request["handler_payload"].(map[string]any)
		image, _ := payload["image"].(string)
		if !strings.Contains(image, "/busybox") {
			continue
		}
		sawBusybox = true
		args, _ := payload["args"].([]any)
		joined := strings.Join(toStrings(args), " ")
		if !strings.Contains(joined, "load-postgres.orbitjob-load.svc.cluster.local") {
			t.Errorf("%s: busybox does not target load-postgres: %s", def.CaseID, joined)
		}
	}
	if !sawBusybox {
		t.Fatal("no busybox definitions found")
	}
}

func TestCurlArgsHaveRetryAndTimeouts(t *testing.T) {
	manifest := mustGenerate(t)
	sawCurl := false
	for _, def := range manifest.Definitions {
		payload, _ := def.Request["handler_payload"].(map[string]any)
		image, _ := payload["image"].(string)
		if !strings.Contains(image, "curlimages/curl") {
			continue
		}
		sawCurl = true
		args, _ := payload["args"].([]any)
		joined := strings.ToLower(strings.Join(toStrings(args), " "))
		for _, want := range []string{"--retry-delay", "--connect-timeout", "--max-time"} {
			if !strings.Contains(joined, want) {
				t.Errorf("%s: curl args missing %s: %v", def.CaseID, want, args)
			}
		}
		if strings.Contains(joined, "--retry 2") {
			t.Errorf("%s: curl still uses --retry 2", def.CaseID)
		}
	}
	if !sawCurl {
		t.Fatal("no curl-family definitions found")
	}
}

func TestPostgresHasConnectTimeout(t *testing.T) {
	manifest := mustGenerate(t)
	sawPostgres := false
	for _, def := range manifest.Definitions {
		payload, _ := def.Request["handler_payload"].(map[string]any)
		image, _ := payload["image"].(string)
		if !strings.Contains(image, "/postgres") {
			continue
		}
		sawPostgres = true
		env, _ := payload["env"].(map[string]any)
		if env["PGCONNECT_TIMEOUT"] != "10" {
			t.Errorf("%s: PGCONNECT_TIMEOUT=%v, want 10", def.CaseID, env["PGCONNECT_TIMEOUT"])
		}
		if env["PGSSLMODE"] != "disable" {
			t.Errorf("%s: PGSSLMODE=%v, want disable", def.CaseID, env["PGSSLMODE"])
		}
	}
	if !sawPostgres {
		t.Fatal("no postgres definitions found")
	}
}

func TestKubectlUsesLoadOperationsSA(t *testing.T) {
	manifest := mustGenerate(t)
	sawKubectl := false
	for _, def := range manifest.Definitions {
		payload, _ := def.Request["handler_payload"].(map[string]any)
		image, _ := payload["image"].(string)
		if !strings.Contains(image, "bitnami/kubectl") && !strings.Contains(image, "kubectl") {
			continue
		}
		sawKubectl = true
		if sa := payload["service_account_name"]; sa != "orbitjob-load-operations" {
			t.Errorf("%s: service_account_name=%v, want orbitjob-load-operations", def.CaseID, sa)
		}
		if mount := payload["automount_service_account_token"]; mount != true {
			t.Errorf("%s: automount_service_account_token=%v, want true", def.CaseID, mount)
		}
	}
	if !sawKubectl {
		t.Fatal("no kubectl definitions found")
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
		got, _ := def.Request["timeout_sec"].(int)
		if got != want[def.Category] {
			t.Errorf("%s: timeout_sec=%d, want %d", def.CaseID, got, want[def.Category])
		}
	}
}

func toStrings(values []any) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = fmt.Sprint(v)
	}
	return out
}

// Kubernetes expands $(VAR) in container args but never ${VAR}; the latter
// reaches the process as a literal and curl tasks fail with URL rejected.
func TestWorkloadArgsUseKubeExpandSyntax(t *testing.T) {
	for _, image := range []string{"python", "alpine", "busybox", "postgres", "curl", "kubectl", "unknown"} {
		for _, terminal := range []string{"success", "failed"} {
			for _, arg := range workloadArgs(image, terminal) {
				s, _ := arg.(string)
				if strings.Contains(s, "${") {
					t.Errorf("workloadArgs(%q, %q) contains ${...} literal: %s", image, terminal, s)
				}
			}
		}
	}
}

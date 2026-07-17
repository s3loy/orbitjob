package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "preflight":
		err = runPreflight(os.Args[2:])
	case "generate":
		err = runGenerate(os.Args[2:])
	case "prepare":
		err = runPrepare(os.Args[2:])
	case "run":
		err = runRun(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	case "report":
		err = runReport(os.Args[2:])
	case "clean":
		err = runClean(os.Args[2:])
	case "monitor":
		err = runMonitor(os.Args[2:])
	case "reset":
		err = runReset(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runPreflight(args []string) error {
	flags := flag.NewFlagSet("preflight", flag.ContinueOnError)
	configPath := flags.String("config", "test/load/config/standard.yaml", "load config")
	imagesPath := flags.String("images", "test/load/config/images.lock.yaml", "image lock")
	profile := flags.String("profile", "standard", "load profile: standard|smoke|long")
	checkOnly := flags.Bool("check-only", false, "skip image pulls and cluster mutations")
	imagesOnly := flags.Bool("images-only", false, "validate image lock only")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if _, err := LoadImageLock(*imagesPath); err != nil {
		return err
	}
	if *imagesOnly {
		fmt.Println("images: 6 lock entries valid")
		return nil
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if err := validateProfile(*profile, cfg); err != nil {
		return err
	}
	if err := requireTools(); err != nil {
		return err
	}
	env, err := inspectDockerEnvironment(context.Background())
	if err != nil {
		return err
	}
	minCPU, minMemBytes := 10, int64(8<<30)
	if *profile == "smoke" {
		minCPU, minMemBytes = 6, 8<<30
	}
	evaluation := EvaluateEnvironmentWith(env, minCPU, minMemBytes)
	if evaluation.Status != "ready" {
		return fmt.Errorf("%s\n%s", evaluation.Status, evaluation.Message)
	}
	// Standard qualification runs require a clean workspace for reproducibility;
	// smoke runs are dev iterations and may carry uncommitted changes.
	if *profile == "standard" {
		output, err := exec.Command("git", "status", "--porcelain=v1", "--untracked-files=all").Output()
		if err != nil {
			return err
		}
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		gitEvaluation := EvaluateGit(lines)
		if gitEvaluation.Verdict != VerdictPass {
			return fmt.Errorf("INCONCLUSIVE: %s", gitEvaluation.Message)
		}
	}
	fmt.Printf("preflight: profile=%s Docker capacity >= %d CPU / %.0f GiB\n", *profile, minCPU, float64(minMemBytes)/(1<<30))
	fmt.Printf("preflight: configuration and image lock valid\n")
	if *checkOnly {
		return nil
	}
	return nil
}

func runGenerate(args []string) error {
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	configPath := flags.String("config", "test/load/config/standard.yaml", "load config")
	imagesPath := flags.String("images", "test/load/config/images.lock.yaml", "image lock")
	scenariosPath := flags.String("scenarios", "test/load/scenarios", "scenario directory")
	runID := flags.String("run-id", "generation-check", "run ID")
	runRoot := flags.String("run-root", "test/load/runs", "run root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	lock, err := LoadImageLock(*imagesPath)
	if err != nil {
		return err
	}
	scenarios, err := LoadScenarios(*scenariosPath)
	if err != nil {
		return err
	}
	manifest, err := Generate(cfg, scenarios, lock)
	if err != nil {
		return err
	}
	dir := filepath.Join(*runRoot, *runID, "generated")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeStableJSON(filepath.Join(dir, "definitions.json"), manifest.Definitions); err != nil {
		return err
	}
	counts := map[string]int{}
	for _, definition := range manifest.Definitions {
		counts[definition.Category]++
	}
	return writeStableJSON(filepath.Join(dir, "summary.json"), map[string]any{
		"definitions": len(manifest.Definitions), "categories": counts, "seed": cfg.Seed,
	})
}

func runPrepare(args []string) error {
	flags := flag.NewFlagSet("prepare", flag.ContinueOnError)
	configPath := flags.String("config", "test/load/config/standard.yaml", "load config")
	imagesPath := flags.String("images", "test/load/config/images.lock.yaml", "image lock")
	profile := flags.String("profile", "standard", "load profile: standard|smoke|long")
	runID := flags.String("run-id", "", "run ID")
	runRoot := flags.String("run-root", "test/load/runs", "run root")
	apiURL := flags.String("api-url", os.Getenv("ORBITJOB_API_URL"), "Admin API URL")
	bootstrapKey := flags.String("bootstrap-key", os.Getenv("ORBITJOB_API_KEY"), "bootstrap admin API key")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *runID == "" || *apiURL == "" || *bootstrapKey == "" {
		return fmt.Errorf("run-id, api-url and bootstrap-key are required")
	}
	return Prepare(*configPath, *imagesPath, *runID, *runRoot, *apiURL, *bootstrapKey, *profile)
}

func runRun(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := flags.String("config", "test/load/config/standard.yaml", "load config")
	profile := flags.String("profile", "standard", "load profile: standard|smoke|long")
	runID := flags.String("run-id", "", "run ID")
	runRoot := flags.String("run-root", "test/load/runs", "run root")
	apiURL := flags.String("api-url", os.Getenv("ORBITJOB_API_URL"), "Admin API URL")
	prometheusURL := flags.String("prometheus-url", "http://localhost:9090", "Prometheus URL for feedback controller")
	tuneDeployments := flags.Bool("tune-deployments", false, "patch worker/admin-api env from resource model")
	dryRunTuning := flags.Bool("dry-run-tuning", false, "print computed tuning and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *runID == "" || *apiURL == "" {
		return fmt.Errorf("run-id and api-url are required")
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := validateProfile(*profile, cfg); err != nil {
		return fmt.Errorf("validate profile: %w", err)
	}
	if *tuneDeployments && !cfg.DynamicTuningEnabled() {
		return fmt.Errorf("--tune-deployments requires dynamic.enabled=true in the load config; static profiles have no tuning model to apply")
	}

	runDir := filepath.Join(*runRoot, *runID)
	created, err := loadCreatedDefinitions(filepath.Join(runDir, "created-definitions.json"))
	if err != nil {
		return fmt.Errorf("load created definitions: %w", err)
	}
	tenantKeys, err := loadTenantKeys(filepath.Join(runDir, "tenant-keys.json"))
	if err != nil {
		return fmt.Errorf("load tenant keys: %w", err)
	}
	clients := make(map[string]*APIClient, len(tenantKeys))
	for tenant, key := range tenantKeys {
		clients[tenant] = NewAPIClient(*apiURL, key)
	}

	var schedule PhaseSchedule
	var maxActive map[string]int
	var pace *PaceController
	var promClient *PrometheusClient

	if cfg.DynamicTuningEnabled() {
		snap, err := InspectResources(context.Background(), cfg.Dynamic.Resource)
		if err != nil {
			return fmt.Errorf("inspect resources: %w", err)
		}
		params, err := ComputeTuning(snap, cfg, cfg.Dynamic.Resource)
		if err != nil {
			return fmt.Errorf("compute tuning: %w", err)
		}
		printTuning(params)
		if *dryRunTuning {
			return nil
		}
		if *tuneDeployments {
			if err := patchDeploymentEnv(context.Background(), "orbitjob-worker", map[string]string{
				"WORKER_CAPACITY":                   strconv.Itoa(params.RecommendedWorkerCapacity),
				"WORKER_CAPACITY_MAX":               strconv.Itoa(params.EffectiveWorkerCapacityMax),
				"WORKER_ADAPTIVE_CAPACITY_ENABLED": "false",
			}); err != nil {
				return fmt.Errorf("patch worker: %w", err)
			}
			if err := patchDeploymentEnv(context.Background(), "orbitjob-admin-api", map[string]string{
				"RATELIMIT_TRIGGER_RPS": strconv.Itoa(params.RecommendedTriggerRPSPerTenant),
			}); err != nil {
				return fmt.Errorf("patch admin-api: %w", err)
			}
		}
		cfg = applyTuningToConfig(cfg, params)
		maxActive = params.MaxActive
		promClient = NewPrometheusClient(*prometheusURL)
		pace = NewPaceController(cfg.Dynamic.Feedback, params, cfg.MinimumInstances, cfg.Duration)
	} else {
		maxActive = map[string]int{}
		for _, p := range cfg.Phases {
			maxActive[p.Name] = p.MaxActive
		}
	}
	schedule = BuildPhaseSchedule(cfg, created)

	var engine *RunEngine
	if pace != nil {
		engine = NewRunEngineWithPace(clients, schedule, maxActive, pace, promClient)
	} else {
		engine = NewRunEngine(clients, schedule, maxActive)
	}

	start := time.Now()
	timeout := cfg.Duration + 30*time.Minute
	if cfg.DynamicTuningEnabled() {
		// Allow stretched schedule when under pressure.
		timeout = time.Duration(float64(cfg.Duration)/cfg.Dynamic.Feedback.MinPace) + 30*time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	faults := &FaultRecorder{}
	go injectFaults(ctx, start, FaultPlanFromConfig(cfg), faults)
	runErr := engine.Run(ctx, func() time.Duration { return time.Since(start) })
	engine.Stop()
	triggered, accepted, rejected, skipped := engine.Stats()
	fmt.Printf("run: triggered=%d accepted=%d rejected=%d skipped=%d\n", triggered, accepted, rejected, skipped)
	stats := RunStats{
		RunID:           *runID,
		StartedAt:       start,
		FinishedAt:      time.Now(),
		ScheduledEvents: len(schedule.Events),
		Triggered:       triggered,
		Accepted:        accepted,
		Rejected:        rejected,
		Skipped:         skipped,
		Breakdown:       engine.Rejections(),
		Completed:       runErr == nil,
		Faults:          faults.Records(),
	}
	if rejected > 0 {
		fmt.Printf("run: rejections: rate_limited=%d server=%d transport=%d other=%d\n",
			stats.Breakdown.RateLimited, stats.Breakdown.Server, stats.Breakdown.Transport, stats.Breakdown.Other)
	}
	if !stats.Completed {
		fmt.Printf("run: TRUNCATED before schedule completed: %v\n", runErr)
	}
	if err := writeStableJSON(filepath.Join(runDir, "run-stats.json"), stats); err != nil {
		return fmt.Errorf("write run stats: %w", err)
	}
	if runErr != nil && runErr != context.DeadlineExceeded {
		return fmt.Errorf("run engine: %w", runErr)
	}
	return nil
}

func printTuning(params TuningParameters) {
	fmt.Printf("tuning: worker_capacity_max=%d worker_capacity=%d trigger_rps_per_tenant=%d\n",
		params.EffectiveWorkerCapacityMax, params.RecommendedWorkerCapacity, params.RecommendedTriggerRPSPerTenant)
	fmt.Printf("tuning: estimated_manual=%d estimated_cron=%d\n",
		params.EstimatedManualInstances, params.EstimatedCronInstances)
	fmt.Println("tuning: phase_rates")
	for name, rate := range params.PhaseRates {
		fmt.Printf("  %s: rate=%d/min max_active=%d\n", name, rate, params.MaxActive[name])
	}
}

func applyTuningToConfig(cfg Config, params TuningParameters) Config {
	for i := range cfg.Phases {
		if r, ok := params.PhaseRates[cfg.Phases[i].Name]; ok {
			cfg.Phases[i].RatePerMinute = r
		}
		if m, ok := params.MaxActive[cfg.Phases[i].Name]; ok {
			cfg.Phases[i].MaxActive = m
		}
	}
	return cfg
}

func patchDeploymentEnv(ctx context.Context, name string, env map[string]string) error {
	if len(env) == 0 {
		return nil
	}
	args := []string{"set", "env", "deployment", name, "-n", "orbitjob"}
	for k, v := range env {
		args = append(args, fmt.Sprintf("%s=%s", k, v))
	}
	out, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl %s: %s", args, strings.TrimSpace(string(out)))
	}
	waitArgs := []string{"rollout", "status", "deployment", name, "-n", "orbitjob", "--timeout=120s"}
	out, err = exec.CommandContext(ctx, "kubectl", waitArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl rollout %s: %s", name, strings.TrimSpace(string(out)))
	}
	return nil
}

func injectFaults(ctx context.Context, start time.Time, plan []FaultSpec, rec *FaultRecorder) {
	for _, fault := range plan {
		wait := fault.Offset - time.Since(start)
		if wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		cmd, restore, err := FaultInjectionPlan(fault.Name, "orbitjob")
		if err != nil || len(cmd) == 0 {
			if err != nil {
				rec.add(FaultRecord{Name: fault.Name, InjectedAt: time.Now().UTC().Format(time.RFC3339), InjectError: err.Error()})
			}
			continue
		}
		fmt.Printf("fault: injecting %s at %s\n", fault.Name, time.Since(start).Round(time.Second))
		injectStart := time.Now()
		record := FaultRecord{Name: fault.Name, InjectedAt: injectStart.UTC().Format(time.RFC3339)}
		if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
			record.InjectError = fmt.Sprintf("%v: %s", err, strings.TrimSpace(string(out)))
			rec.add(record)
			fmt.Printf("fault: %s injection failed: %s\n", fault.Name, record.InjectError)
			continue
		}
		if fault.DisconnectFor > 0 && len(restore) > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(fault.DisconnectFor):
			}
			if out, err := exec.Command(restore[0], restore[1:]...).CombinedOutput(); err != nil {
				record.InjectError = fmt.Sprintf("restore: %v: %s", err, strings.TrimSpace(string(out)))
				rec.add(record)
				fmt.Printf("fault: %s restore failed: %s\n", fault.Name, record.InjectError)
				continue
			}
		}
		gate := RecoveryGate{Name: fault.Name, ReadyTimeout: 2 * time.Minute, BusinessTimeout: 5 * time.Minute}
		if err := WaitForRecovery(ctx, gate, deploymentReadyCheck(fault.Name, "orbitjob")); err != nil {
			record.InjectError = err.Error()
		}
		record.RecoveredSeconds = time.Since(injectStart).Seconds()
		rec.add(record)
		fmt.Printf("fault: %s recovered in %.0fs\n", fault.Name, record.RecoveredSeconds)
	}
}

// deploymentReadyCheck polls the target deployment until it reports one ready
// replica, twice in a row (handled by WaitForRecovery).
func deploymentReadyCheck(name, namespace string) func(context.Context) bool {
	return func(ctx context.Context) bool {
		out, err := exec.CommandContext(ctx, "kubectl", "get", "deployment", "orbitjob-"+name,
			"-n", namespace, "-o", "jsonpath={.status.readyReplicas}").Output()
		if err != nil {
			return false
		}
		return strings.TrimSpace(string(out)) == "1"
	}
}

func runReport(args []string) error {
	flags := flag.NewFlagSet("report", flag.ContinueOnError)
	runID := flags.String("run-id", "", "run ID")
	runRoot := flags.String("run-root", "test/load/runs", "run root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *runID == "" {
		return fmt.Errorf("run-id is required")
	}
	runDir := filepath.Join(*runRoot, *runID)
	data, err := os.ReadFile(filepath.Join(runDir, "result.json"))
	if err != nil {
		return fmt.Errorf("read result: %w", err)
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}

	// verify writes run-record.json with the run's true environment and start
	// time; fall back to a minimal record for run dirs predating it.
	var run RunRecord
	if recordData, err := os.ReadFile(filepath.Join(runDir, "run-record.json")); err == nil {
		if err := json.Unmarshal(recordData, &run); err != nil {
			return fmt.Errorf("decode run record: %w", err)
		}
	} else {
		run = RunRecord{
			RunID:         *runID,
			Qualification: result.Qualification,
			Commit:        gitCommit(),
			Dirty:         gitDirty(),
			StartedAt:     result.CompletedAt,
		}
	}

	var stats *RunStats
	if s, err := loadRunStats(filepath.Join(runDir, "run-stats.json")); err == nil {
		stats = &s
	}
	if err := WriteReport(runDir, run, result, stats); err != nil {
		return err
	}
	if err := WriteChecksums(runDir); err != nil {
		return err
	}
	fmt.Printf("report: verdict=%s written to %s\n", result.Verdict, runDir)
	return nil
}

func runClean(args []string) error {
	flags := flag.NewFlagSet("clean", flag.ContinueOnError)
	runID := flags.String("run-id", "", "run ID")
	runRoot := flags.String("run-root", "test/load/runs", "run root")
	confirm := flags.Bool("confirm", false, "confirm cleanup")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *runID == "" || !*confirm {
		return fmt.Errorf("run-id and --confirm are required")
	}
	if err := ValidateCleanRunID(*runID); err != nil {
		return err
	}
	for _, manifest := range []string{"fixture.yaml", "postgres.yaml", "fixture-configmap.yaml", "operations-rbac.yaml", "namespace.yaml"} {
		_ = exec.Command("kubectl", "delete", "-f", filepath.Join("deploy/load", manifest), "--ignore-not-found").Run()
	}
	runDir := filepath.Join(*runRoot, *runID)
	if err := os.RemoveAll(runDir); err != nil {
		return fmt.Errorf("remove run dir: %w", err)
	}
	fmt.Printf("clean: removed %s\n", runDir)
	return nil
}

func writeStableJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func runMonitor(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("monitor requires deploy|status|cleanup")
	}
	switch args[0] {
	case "deploy":
		return monitorDeploy()
	case "status":
		return monitorStatus()
	case "cleanup":
		return monitorCleanup()
	default:
		return fmt.Errorf("unknown monitor action %q: deploy|status|cleanup", args[0])
	}
}

func monitorDeploy() error {
	// Apply the whole directory so new manifests (dashboards, exporters) are
	// picked up without touching this list.
	if err := kubectlApply("deploy/monitoring"); err != nil {
		return fmt.Errorf("apply monitoring: %w", err)
	}
	if err := kubectlWait("deployment/prometheus", "monitoring", 3*time.Minute); err != nil {
		return fmt.Errorf("wait prometheus: %w", err)
	}
	if err := kubectlWait("deployment/grafana", "monitoring", 3*time.Minute); err != nil {
		return fmt.Errorf("wait grafana: %w", err)
	}
	if err := kubectlWait("deployment/orbitjob-postgres-exporter", "orbitjob", 3*time.Minute); err != nil {
		return fmt.Errorf("wait postgres exporter: %w", err)
	}
	fmt.Println("monitor: prometheus + grafana + postgres exporter deployed")
	fmt.Println("monitor: view with port-forward:")
	fmt.Println("  kubectl port-forward -n monitoring svc/grafana 3000:3000  (admin/admin)")
	fmt.Println("  kubectl port-forward -n monitoring svc/prometheus 9090:9090")
	return nil
}

func monitorStatus() error {
	out, err := exec.Command("kubectl", "get", "pods", "-n", "monitoring").CombinedOutput()
	if err != nil {
		return fmt.Errorf("get pods: %s", strings.TrimSpace(string(out)))
	}
	fmt.Print(string(out))
	return nil
}

func monitorCleanup() error {
	out, err := exec.Command("kubectl", "delete", "-f", "deploy/monitoring", "--ignore-not-found").CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete monitoring: %s", strings.TrimSpace(string(out)))
	}
	fmt.Println("monitor: removed")
	return nil
}

func validateProfile(profile string, cfg Config) error {
	switch profile {
	case "standard":
		return ValidateStandard(cfg)
	case "smoke":
		return ValidateSmoke(cfg)
	case "long":
		return ValidateLong(cfg)
	default:
		return fmt.Errorf("unknown profile %q: standard|smoke|long", profile)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: loadtest <preflight|generate|prepare|run|verify|report|clean|monitor|reset> [flags]")
}

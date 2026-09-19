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
	// A qualification run has to stay awake; on darwin the caffeinate assertion
	// only does that on AC power (sleep.go), so refuse a battery up front.
	if err := requireACPower(context.Background(), cfg); err != nil {
		return err
	}
	fmt.Printf("preflight: profile=%s Docker capacity >= %d CPU / %.0f GiB\n", *profile, minCPU, float64(minMemBytes)/(1<<30))
	fmt.Printf("preflight: configuration and image lock valid\n")
	// A profile with no gate cannot fail one, so only static profiles that
	// declare minimum_instances are estimated.
	if cfg.MinimumInstances > 0 {
		if estimate, ok := EstimateStaticRuns(cfg); ok {
			if estimate.Total < cfg.MinimumInstances {
				return fmt.Errorf("INCONCLUSIVE: static schedule can produce about %d instances (manual %d + burst %d + cron %d), below the minimum_instances gate of %d; the phases or rates cannot qualify this profile", estimate.Total, estimate.Manual, estimate.Burst, estimate.Cron, cfg.MinimumInstances)
			}
			fmt.Printf("preflight: static instance estimate ~%d (manual %d + burst %d + cron %d) >= minimum_instances %d\n", estimate.Total, estimate.Manual, estimate.Burst, estimate.Cron, cfg.MinimumInstances)
		}
	}
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
	// The same tenant-mode gate preflight, prepare and run apply. Without it
	// generate happily writes a manifest for a profile the next step refuses,
	// which reads as a failure of the next step rather than of the profile.
	if err := ValidateTenants(cfg); err != nil {
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
	tuneDeployments := flags.Bool("tune-deployments", false, "patch admin-api env from resource model")
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
		if *dryRunTuning {
			return nil
		}
		if *tuneDeployments {
			// There is no worker deployment to size any more; execution
			// concurrency is bounded by the task namespace quota, which the
			// load tool applies to itself through MaxActive. The only
			// control-plane knob left to patch is the admin API's trigger rate
			// limit, which caps how fast the generator can submit.
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
	release := holdSleepAssertion()
	defer release()
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
	reportSuspension(start, time.Since(start))
	triggered, accepted, rejected, skipped := engine.Stats()
	fmt.Printf("run: triggered=%d accepted=%d rejected=%d skipped=%d\n", triggered, accepted, rejected, skipped)
	if canceled, failed := engine.CancelStats(); canceled > 0 || failed > 0 {
		fmt.Printf("run: canceled=%d cancel_failed=%d\n", canceled, failed)
	}
	stats := RunStats{
		RunID:           *runID,
		Profile:         cfg.Profile,
		Seed:            cfg.Seed,
		Qualification:   cfg.Qualification,
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
	args := []string{"set", "env", "deployment", name, "-n", WorkloadNamespace()}
	for k, v := range env {
		args = append(args, fmt.Sprintf("%s=%s", k, v))
	}
	out, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl %s: %s", args, strings.TrimSpace(string(out)))
	}
	waitArgs := []string{"rollout", "status", "deployment", name, "-n", WorkloadNamespace(), "--timeout=120s"}
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
		namespace := FaultNamespace(fault.Name)
		cmd, restore, err := FaultInjectionPlan(fault.Name, namespace)
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
		// A restore step runs whenever one exists. Gating it on DisconnectFor
		// meant a fault declared in a config -- which carries only a name and an
		// offset -- scaled PostgreSQL to zero and left it there: the plan
		// supplies no DisconnectFor, so the branch never ran.
		if len(restore) > 0 {
			if fault.DisconnectFor > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(fault.DisconnectFor):
				}
			}
			if out, err := exec.Command(restore[0], restore[1:]...).CombinedOutput(); err != nil {
				record.InjectError = fmt.Sprintf("restore: %v: %s", err, strings.TrimSpace(string(out)))
				rec.add(record)
				fmt.Printf("fault: %s restore failed: %s\n", fault.Name, record.InjectError)
				continue
			}
		}
		gate := RecoveryGate{Name: fault.Name, ReadyTimeout: 2 * time.Minute, BusinessTimeout: 5 * time.Minute}
		if err := WaitForRecovery(ctx, gate, deploymentReadyCheck(fault.Name, namespace)); err != nil {
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
			"-n", namespace, "-o", "json").Output()
		if err != nil {
			return false
		}
		ready, err := decodeReadyReplicas(out)
		if err != nil {
			return false
		}
		return ready == 1
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

func validateProfile(profile string, cfg Config) error {
	// Tenant-mode validation runs before the profile switch so a profile
	// declaring an unsupported runtime is refused with that reason even when
	// the profile name itself is not one the switch knows.
	if err := ValidateTenants(cfg); err != nil {
		return err
	}
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
	fmt.Fprintln(os.Stderr, "usage: loadtest <preflight|generate|prepare|run|verify|report|clean|reset> [flags]")
}

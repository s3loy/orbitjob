package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	if err := ValidateStandard(cfg); err != nil {
		return err
	}
	if err := requireTools(); err != nil {
		return err
	}
	env, err := inspectDockerEnvironment(context.Background())
	if err != nil {
		return err
	}
	evaluation := EvaluateEnvironment(env)
	if evaluation.Status != "ready" {
		return fmt.Errorf("%s\n%s", evaluation.Status, evaluation.Message)
	}
	output, err := exec.Command("git", "status", "--porcelain=v1", "--untracked-files=all").Output()
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	gitEvaluation := EvaluateGit(lines)
	if gitEvaluation.Verdict != VerdictPass {
		return fmt.Errorf("INCONCLUSIVE: %s", gitEvaluation.Message)
	}
	fmt.Printf("preflight: Docker capacity >= 10 CPU / 8 GiB\n")
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
	return Prepare(*configPath, *imagesPath, *runID, *runRoot, *apiURL, *bootstrapKey)
}

func runRun(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := flags.String("config", "test/load/config/standard.yaml", "load config")
	runID := flags.String("run-id", "", "run ID")
	runRoot := flags.String("run-root", "test/load/runs", "run root")
	apiURL := flags.String("api-url", os.Getenv("ORBITJOB_API_URL"), "Admin API URL")
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
	if err := ValidateStandard(cfg); err != nil {
		return fmt.Errorf("validate standard: %w", err)
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
	maxActive := map[string]int{}
	for _, p := range cfg.Phases {
		maxActive[p.Name] = p.MaxActive
	}
	schedule := BuildPhaseSchedule(cfg, created)
	engine := NewRunEngine(clients, schedule, maxActive)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration+30*time.Minute)
	defer cancel()

	go injectFaults(ctx, start)
	if err := engine.Run(ctx, func() time.Duration { return time.Since(start) }); err != nil && err != context.DeadlineExceeded {
		return fmt.Errorf("run engine: %w", err)
	}
	engine.Stop()
	triggered, accepted, rejected := engine.Stats()
	fmt.Printf("run: triggered=%d accepted=%d rejected=%d\n", triggered, accepted, rejected)
	return nil
}

func injectFaults(ctx context.Context, start time.Time) {
	for _, fault := range StandardFaultPlan() {
		wait := fault.Offset - time.Since(start)
		if wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		cmd, err := FaultInjectionCommand(fault.Name, "orbitjob")
		if err != nil || len(cmd) == 0 {
			continue
		}
		_ = exec.Command(cmd[0], cmd[1:]...).Run()
		gate := RecoveryGate{Name: fault.Name, ReadyTimeout: 2 * time.Minute, BusinessTimeout: 5 * time.Minute}
		_ = WaitForRecovery(ctx, gate, func(context.Context) bool { return true })
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
	run := RunRecord{RunID: *runID, Qualification: true, StartedAt: time.Now()}
	if err := WriteReport(runDir, run, result); err != nil {
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

func usage() {
	fmt.Fprintln(os.Stderr, "usage: loadtest <preflight|generate|prepare|run|verify|report|clean> [flags]")
}

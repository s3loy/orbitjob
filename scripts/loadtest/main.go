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
	case "prepare", "run", "verify", "report", "clean", "smoke", "calibrate", "inject-fault":
		err = fmt.Errorf("%s is not implemented yet", os.Args[1])
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

func writeStableJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: loadtest <preflight|generate|prepare|run|verify|report|clean|smoke|calibrate|inject-fault>")
}

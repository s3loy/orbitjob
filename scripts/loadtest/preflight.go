package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type EnvironmentEvaluation struct {
	Status  string
	Message string
}

// StaticRunEstimate is the run count a static (non-dynamic) profile is
// projected to produce: one trigger per phase schedule slot, the peak burst, and
// the cron baseline. It exists so a profile whose phases cannot reach its own
// minimum_instances gate fails in seconds instead of after a four-hour run.
type StaticRunEstimate struct {
	Manual int
	Burst  int
	Cron   int
	Total  int
}

// EstimateStaticRuns projects the run count for a static profile. The
// phase slots mirror BuildPhaseSchedule's pacing, so the estimate tracks the
// schedule rather than an average rate. A dynamic profile has no literal rates
// to project, so it returns ok=false and the caller skips the check.
func EstimateStaticRuns(cfg Config) (StaticRunEstimate, bool) {
	if cfg.Dynamic.Enabled {
		return StaticRunEstimate{}, false
	}
	var estimate StaticRunEstimate
	for _, phase := range cfg.Phases {
		if phase.RatePerMinute <= 0 {
			continue
		}
		interval := time.Minute / time.Duration(phase.RatePerMinute)
		if interval <= 0 {
			continue
		}
		for offset := phase.Offset; offset < phase.Offset+phase.Duration; offset += interval {
			estimate.Manual++
		}
	}
	estimate.Burst = cfg.Burst.Count
	estimate.Cron = EstimatedCronRuns(cfg)
	estimate.Total = estimate.Manual + estimate.Burst + estimate.Cron
	return estimate, true
}

// PowerEvaluation reports whether the host can hold off system sleep. Status is
// "ready" or "on battery".
type PowerEvaluation struct {
	Status  string
	Message string
}

// EvaluatePowerSource parses `pmset -g batt` output. caffeinate -s only holds
// off system sleep while the host is on AC power (sleep.go), so a qualification
// run on a battery silently stretches and its latency numbers are unusable. Any
// output that does not name Battery Power -- AC, a desktop with no battery, or
// an unparsable string -- is treated as usable rather than blocking a run the
// tool cannot judge.
func EvaluatePowerSource(output string) PowerEvaluation {
	if strings.Contains(output, "Battery Power") {
		return PowerEvaluation{
			Status:  "on battery",
			Message: "qualification run refused: the host is on battery power and caffeinate -s cannot hold off system sleep there; connect AC power or run a non-qualification profile",
		}
	}
	return PowerEvaluation{Status: "ready"}
}

// requireACPower refuses a qualification run on darwin when the host is on
// battery. Linux has no caffeinate assertion to defeat, and a non-qualification
// profile may run on a battery, so both are left alone.
func requireACPower(ctx context.Context, cfg Config) error {
	if !cfg.Qualification || runtime.GOOS != "darwin" {
		return nil
	}
	output, err := exec.CommandContext(ctx, "pmset", "-g", "batt").Output()
	if err != nil {
		return fmt.Errorf("pmset -g batt: %w", err)
	}
	if evaluation := EvaluatePowerSource(string(output)); evaluation.Status != "ready" {
		return errors.New(evaluation.Message)
	}
	return nil
}

type GitEvaluation struct {
	Verdict Verdict
	Message string
}

func EvaluateEnvironment(env Environment) EnvironmentEvaluation {
	return EvaluateEnvironmentWith(env, 10, 8<<30)
}

// EvaluateEnvironmentWith checks Docker capacity against profile-specific
// minimums. Standard requires 10 CPU / 8 GiB for reproducible 4h runs; smoke
// relaxes to 6 CPU / 8 GiB so a dev machine can exercise the pipeline without
// the full qualification footprint.
func EvaluateEnvironmentWith(env Environment, minCPU int, minMemBytes int64) EnvironmentEvaluation {
	if env.DockerCPU < minCPU || env.DockerMemoryBytes < minMemBytes {
		return EnvironmentEvaluation{
			Status:  "environment rejected",
			Message: fmt.Sprintf("required: Docker %d CPU / %.2f GiB; observed: %d CPU / %.2f GiB", minCPU, float64(minMemBytes)/(1<<30), env.DockerCPU, float64(env.DockerMemoryBytes)/(1<<30)),
		}
	}
	return EnvironmentEvaluation{Status: "ready"}
}

func EvaluateGit(lines []string) GitEvaluation {
	for _, line := range lines {
		path := strings.TrimSpace(line)
		if path == "" || strings.Contains(path, "test/load/runs/") {
			continue
		}
		return GitEvaluation{Verdict: VerdictInconclusive, Message: "workspace contains source or configuration changes"}
	}
	return GitEvaluation{Verdict: VerdictPass}
}

func inspectDockerEnvironment(ctx context.Context) (Environment, error) {
	output, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.NCPU}} {{.MemTotal}}").Output()
	if err != nil {
		return Environment{}, fmt.Errorf("docker info: %w", err)
	}
	fields := strings.Fields(string(output))
	if len(fields) != 2 {
		return Environment{}, fmt.Errorf("unexpected docker info output %q", output)
	}
	cpu, err := strconv.Atoi(fields[0])
	if err != nil {
		return Environment{}, err
	}
	memory, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return Environment{}, err
	}
	return Environment{DockerCPU: cpu, DockerMemoryBytes: memory}, nil
}

func requireTools() error {
	for _, tool := range []string{"docker", "kind", "kubectl", "helm", "go", "curl", "git", "docker"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("required tool %s is not installed", tool)
		}
	}
	return nil
}

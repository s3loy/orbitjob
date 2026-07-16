package main

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type EnvironmentEvaluation struct {
	Status  string
	Message string
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

package main

import "testing"

func TestEvaluateEnvironmentRejectsLowDockerCapacity(t *testing.T) {
	got := EvaluateEnvironment(Environment{DockerCPU: 8, DockerMemoryBytes: 8 << 30})
	if got.Status != "environment rejected" {
		t.Fatalf("status = %q", got.Status)
	}
}

func TestEvaluateEnvironmentAcceptsStandardCapacity(t *testing.T) {
	got := EvaluateEnvironment(Environment{DockerCPU: 10, DockerMemoryBytes: 8 << 30})
	if got.Status != "ready" {
		t.Fatalf("status = %q", got.Status)
	}
}

func TestEvaluateGitRejectsProductChanges(t *testing.T) {
	got := EvaluateGit([]string{" M internal/core/app/execute/tick.go"})
	if got.Verdict != VerdictInconclusive {
		t.Fatalf("verdict = %s", got.Verdict)
	}
}

func TestEvaluateGitAllowsRunEvidence(t *testing.T) {
	got := EvaluateGit([]string{"?? test/load/runs/run-1/result.json"})
	if got.Verdict != VerdictPass {
		t.Fatalf("verdict = %s", got.Verdict)
	}
}

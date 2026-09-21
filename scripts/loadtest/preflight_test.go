package main

import (
	"strings"
	"testing"
)

// TestStaticRunEstimate pins the arithmetic for the standard profile: the
// phase schedule, the burst, and the cron baseline must clear minimum_instances,
// and a static profile whose phases fall short must be reported below the gate.
func TestStaticRunEstimate(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/standard.yaml")
	if err != nil {
		t.Fatal(err)
	}
	estimate, ok := EstimateStaticRuns(cfg)
	if !ok {
		t.Fatal("standard profile is static; estimate should apply")
	}
	// manual 5550 + burst 500 + cron 360 * (4h/10m) = 14690.
	if estimate.Manual != 5550 || estimate.Burst != 500 || estimate.Cron != 8640 || estimate.Total != 14690 {
		t.Fatalf("estimate = %#v", estimate)
	}
	if estimate.Total < cfg.MinimumInstances {
		t.Fatalf("estimate %d below gate %d", estimate.Total, cfg.MinimumInstances)
	}

	// A profile whose phases cannot reach its own gate.
	short := cfg
	short.MinimumInstances = 10000
	for i := range short.Phases {
		short.Phases[i].RatePerMinute = 1
	}
	short.Burst.Count = 0
	short.Definitions.ProductTriggerTypes = map[string]int{"cron": 0}
	if shortEstimate, ok := EstimateStaticRuns(short); !ok || shortEstimate.Total >= short.MinimumInstances {
		t.Fatalf("short estimate = %#v, want below %d", shortEstimate, short.MinimumInstances)
	}
}

func TestStaticRunEstimateSkipsDynamicProfiles(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/standard-dynamic.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := EstimateStaticRuns(cfg); ok {
		t.Fatal("dynamic profile must not be estimated statically")
	}
}

// TestQualificationRequiresACPower checks the injected pmset output: a battery
// refuses a qualification run, and AC or an unreadable source does not.
func TestQualificationRequiresACPower(t *testing.T) {
	onBattery := "Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t85%; discharging; 3:20 remaining present: true\n"
	evaluation := EvaluatePowerSource(onBattery)
	if evaluation.Status == "ready" {
		t.Fatalf("battery output accepted: %#v", evaluation)
	}
	if !strings.Contains(evaluation.Message, "battery") {
		t.Fatalf("message = %q", evaluation.Message)
	}

	onAC := "Now drawing from 'AC Power'\n -InternalBattery-0 (id=1)\t100%; charged; 0:00 remaining present: true\n"
	if got := EvaluatePowerSource(onAC); got.Status != "ready" {
		t.Fatalf("AC output rejected: %#v", got)
	}

	// A desktop or a CI runner that reports nothing must not be blocked.
	if got := EvaluatePowerSource(""); got.Status != "ready" {
		t.Fatalf("empty output rejected: %#v", got)
	}
}

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

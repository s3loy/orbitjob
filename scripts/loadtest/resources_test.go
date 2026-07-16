package main

import (
	"testing"
	"time"
)

func TestComputeTuning_StandardResources(t *testing.T) {
	cfg := Config{
		Duration:         4 * time.Hour,
		MinimumInstances: 10000,
		Tenants:          []string{"a", "b", "c", "d"},
		Definitions: DefinitionConfig{
			ProductTriggerTypes: map[string]int{"manual": 840, "cron": 360},
			CronIntervalMinutes: 10,
		},
		Phases: []Phase{
			{Name: "warmup", Offset: 0, Duration: 10 * time.Minute},
			{Name: "correctness", Offset: 10 * time.Minute, Duration: 30 * time.Minute},
			{Name: "steady", Offset: 40 * time.Minute, Duration: 30 * time.Minute},
			{Name: "ramp", Offset: 70 * time.Minute, Duration: 30 * time.Minute},
			{Name: "peak", Offset: 100 * time.Minute, Duration: 20 * time.Minute},
			{Name: "recovery", Offset: 120 * time.Minute, Duration: 40 * time.Minute},
			{Name: "fault-recovery", Offset: 160 * time.Minute, Duration: 80 * time.Minute},
		},
	}
	rm := ResourceModelConfig{
		TaskAvgDurationSec: 30,
		SystemReserveCPU:   1,
		SystemReserveMemGi: 2,
		HeadroomFactor:     0.8,
		PeakOvershoot:      1.2,
	}
	snap := ResourceSnapshot{
		DockerCPU:               10,
		DockerMemoryGi:          8,
		TaskNamespaceCPULimit:   10,
		TaskNamespaceMemGiLimit: 8,
		TaskCPULimit:            0.5,
		TaskMemLimitGi:          0.25,
		WorkerCPULimit:          2,
		WorkerMemLimitGi:        1,
		AdminAPICPULimit:        1,
		AdminAPIMemLimitGi:      0.5,
	}

	params, err := ComputeTuning(snap, cfg, rm)
	if err != nil {
		t.Fatalf("ComputeTuning error: %v", err)
	}

	if params.EffectiveWorkerCapacityMax != 14 {
		// availableCPU = min(10, 10-1=9) = 9; availableMem = min(8, 8-2=6)=6
		// cpuBound = 9/0.5=18; memBound=6/0.25=24; raw=18; *0.8=14.4 -> 14
		t.Fatalf("EffectiveWorkerCapacityMax = %d, want 14", params.EffectiveWorkerCapacityMax)
	}
	if params.RecommendedWorkerCapacity != params.EffectiveWorkerCapacityMax {
		t.Fatalf("RecommendedWorkerCapacity mismatch")
	}
	if params.EstimatedCronInstances == 0 {
		t.Fatalf("expected cron instances > 0")
	}
	if params.EstimatedManualInstances < cfg.MinimumInstances-params.EstimatedCronInstances {
		t.Fatalf("manual instances %d below requirement %d", params.EstimatedManualInstances, cfg.MinimumInstances-params.EstimatedCronInstances)
	}
	if params.RecommendedTriggerRPSPerTenant < 10 {
		t.Fatalf("trigger RPS per tenant too low: %d", params.RecommendedTriggerRPSPerTenant)
	}
	if params.PhaseRates["peak"] < params.PhaseRates["warmup"] {
		t.Fatalf("peak rate %d should exceed warmup rate %d", params.PhaseRates["peak"], params.PhaseRates["warmup"])
	}
}

func TestComputeTuning_SmokeResources(t *testing.T) {
	cfg := Config{
		Duration:         30 * time.Minute,
		MinimumInstances: 0,
		Tenants:          []string{"a", "b", "c", "d"},
		Definitions: DefinitionConfig{
			ProductTriggerTypes: map[string]int{"manual": 1200},
		},
		Phases: []Phase{
			{Name: "warmup", Offset: 0, Duration: 2 * time.Minute},
			{Name: "steady", Offset: 2 * time.Minute, Duration: 28 * time.Minute},
		},
	}
	rm := ResourceModelConfig{
		TaskAvgDurationSec: 30,
		SystemReserveCPU:   1,
		SystemReserveMemGi: 2,
		HeadroomFactor:     0.8,
		PeakOvershoot:      1.2,
	}
	snap := ResourceSnapshot{
		DockerCPU:               6,
		DockerMemoryGi:          8,
		TaskNamespaceCPULimit:   10,
		TaskNamespaceMemGiLimit: 8,
		TaskCPULimit:            0.5,
		TaskMemLimitGi:          0.25,
		WorkerCPULimit:          2,
		WorkerMemLimitGi:        1,
		AdminAPICPULimit:        1,
		AdminAPIMemLimitGi:      0.5,
	}

	params, err := ComputeTuning(snap, cfg, rm)
	if err != nil {
		t.Fatalf("ComputeTuning error: %v", err)
	}
	if params.EffectiveWorkerCapacityMax != 8 {
		// availableCPU = min(10, 6-1=5) = 5; availableMem = min(8, 8-2=6)=6
		// cpuBound = 5/0.5=10; memBound=6/0.25=24; raw=10; *0.8=8
		t.Fatalf("EffectiveWorkerCapacityMax = %d, want 8", params.EffectiveWorkerCapacityMax)
	}
}

func TestParseCPU(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"1", 1.0},
		{"500m", 0.5},
		{"250m", 0.25},
		{"2.5", 2.5},
	}
	for _, c := range cases {
		got, err := parseCPU(c.in)
		if err != nil {
			t.Fatalf("parseCPU(%q) error: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("parseCPU(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseMemoryGi(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"1Gi", 1.0},
		{"512Mi", 0.5},
		{"256Mi", 0.25},
		{"1073741824", 1.0},
	}
	for _, c := range cases {
		got, err := parseMemoryGi(c.in)
		if err != nil {
			t.Fatalf("parseMemoryGi(%q) error: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("parseMemoryGi(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

package main

import (
	"strings"
	"testing"
	"time"
)

func TestLoadStandardConfig(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/standard.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Seed != "v020-standard-1" || cfg.Duration != 4*time.Hour {
		t.Fatalf("config = %#v", cfg)
	}
	if cfg.Definitions.Total != 1200 || cfg.MinimumInstances != 10000 {
		t.Fatalf("scale = %#v", cfg.Definitions)
	}
	if err := ValidateStandard(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestValidateStandardRejectsReducedDuration(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/standard.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Duration = 3 * time.Hour
	err = ValidateStandard(cfg)
	if err == nil || !strings.Contains(err.Error(), "exactly 4h0m0s") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateSmoke(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/smoke.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSmoke(cfg); err != nil {
		t.Fatalf("validate smoke: %v", err)
	}
	if cfg.Qualification {
		t.Fatal("smoke profile must have qualification=false")
	}
	if cfg.Duration != 30*time.Minute {
		t.Fatalf("smoke duration = %s, want 30m", cfg.Duration)
	}
}

func TestValidateSmokeRejectsQualificationTrue(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/smoke.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Qualification = true
	err = ValidateSmoke(cfg)
	if err == nil || !strings.Contains(err.Error(), "qualification=false") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateSmokeRejectsWrongDuration(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/smoke.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Duration = 45 * time.Minute
	err = ValidateSmoke(cfg)
	if err == nil || !strings.Contains(err.Error(), "30m0s") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateLong(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/long.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateLong(cfg); err != nil {
		t.Fatalf("validate long: %v", err)
	}
	if cfg.Qualification {
		t.Fatal("long profile must have qualification=false")
	}
	if cfg.Duration != 8*time.Hour {
		t.Fatalf("long duration = %s, want 8h", cfg.Duration)
	}
}

func TestLoadLongConfigAlignsFaultsWithRecoveryPhase(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/long.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Faults.Plan) == 0 {
		t.Fatal("long profile should declare a fault plan")
	}
	// fault-recovery phase spans 360m-480m; faults anywhere else defeat the
	// phase's purpose.
	for _, fault := range cfg.Faults.Plan {
		if fault.Offset < 360*time.Minute || fault.Offset >= 480*time.Minute {
			t.Fatalf("fault %s at %s falls outside fault-recovery phase", fault.Name, fault.Offset)
		}
	}
}

func TestValidateFaultsRejectsUnknownName(t *testing.T) {
	cfg := Config{
		Duration: time.Hour,
		Faults: FaultsConfig{Plan: []FaultPhase{
			{Name: "unknown-component", Offset: time.Minute},
		}},
	}
	err := validateFaults(cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown fault") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateFaultsRejectsNonIncreasingOffsets(t *testing.T) {
	cfg := Config{
		Duration: 2 * time.Hour,
		Faults: FaultsConfig{Plan: []FaultPhase{
			{Name: "worker", Offset: 40 * time.Minute},
			{Name: "postgres", Offset: 30 * time.Minute},
		}},
	}
	err := validateFaults(cfg)
	if err == nil || !strings.Contains(err.Error(), "increasing") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateFaultsRejectsOffsetBeyondDuration(t *testing.T) {
	cfg := Config{
		Duration: time.Hour,
		Faults: FaultsConfig{Plan: []FaultPhase{
			{Name: "worker", Offset: 2 * time.Hour},
		}},
	}
	err := validateFaults(cfg)
	if err == nil || !strings.Contains(err.Error(), "duration") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateStandardRejectsCustomFaultPlan(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/standard.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Faults = FaultsConfig{Plan: []FaultPhase{{Name: "worker", Offset: time.Hour}}}
	err = ValidateStandard(cfg)
	if err == nil || !strings.Contains(err.Error(), "fault") {
		t.Fatalf("error = %v", err)
	}
}

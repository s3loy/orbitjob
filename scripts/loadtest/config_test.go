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

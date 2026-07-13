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

package main

import (
	"testing"
	"time"
)

func TestLoadSchedulerRuntimeConfig_Defaults(t *testing.T) {
	t.Setenv("SCHEDULER_BATCH_SIZE", "")
	t.Setenv("SCHEDULER_TICK_INTERVAL_SEC", "")

	cfg, err := loadSchedulerRuntimeConfig()
	if err != nil {
		t.Fatalf("loadSchedulerRuntimeConfig() error = %v", err)
	}
	if cfg.BatchSize != 100 {
		t.Fatalf("expected default batch size=100, got %d", cfg.BatchSize)
	}
	if cfg.TickInterval != 5*time.Second {
		t.Fatalf("expected default tick interval=5s, got %s", cfg.TickInterval)
	}
}

func TestLoadSchedulerRuntimeConfig_Custom(t *testing.T) {
	t.Setenv("SCHEDULER_BATCH_SIZE", "250")
	t.Setenv("SCHEDULER_TICK_INTERVAL_SEC", "2")

	cfg, err := loadSchedulerRuntimeConfig()
	if err != nil {
		t.Fatalf("loadSchedulerRuntimeConfig() error = %v", err)
	}
	if cfg.BatchSize != 250 {
		t.Fatalf("expected batch size=250, got %d", cfg.BatchSize)
	}
	if cfg.TickInterval != 2*time.Second {
		t.Fatalf("expected tick interval=2s, got %s", cfg.TickInterval)
	}
}

func TestLoadSchedulerRuntimeConfig_InvalidBatchSize(t *testing.T) {
	t.Setenv("SCHEDULER_BATCH_SIZE", "abc")
	t.Setenv("SCHEDULER_TICK_INTERVAL_SEC", "")

	if _, err := loadSchedulerRuntimeConfig(); err == nil {
		t.Fatalf("expected error for invalid batch size")
	}
}

func TestLoadSchedulerRuntimeConfig_InvalidTickInterval(t *testing.T) {
	t.Setenv("SCHEDULER_BATCH_SIZE", "")
	t.Setenv("SCHEDULER_TICK_INTERVAL_SEC", "0")

	if _, err := loadSchedulerRuntimeConfig(); err == nil {
		t.Fatalf("expected error for invalid tick interval")
	}
}

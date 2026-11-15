package main

import (
	"strings"
	"testing"

	"orbitjob/internal/platform/config"
)

// ---------------------------------------------------------------------------
// loadPositiveIntEnv edge cases
// ---------------------------------------------------------------------------

func TestLoadPositiveIntEnv_Zero(t *testing.T) {
	t.Setenv("TEST_SCHEDULER_KEY", "0")

	_, err := config.LoadPositiveIntEnv("TEST_SCHEDULER_KEY", 100)
	if err == nil || !strings.Contains(err.Error(), "must be >= 1") {
		t.Fatalf("expected 'must be >= 1' error, got %v", err)
	}
}

func TestLoadPositiveIntEnv_Negative(t *testing.T) {
	t.Setenv("TEST_SCHEDULER_KEY", "-5")

	_, err := config.LoadPositiveIntEnv("TEST_SCHEDULER_KEY", 100)
	if err == nil || !strings.Contains(err.Error(), "must be >= 1") {
		t.Fatalf("expected 'must be >= 1' error, got %v", err)
	}
}

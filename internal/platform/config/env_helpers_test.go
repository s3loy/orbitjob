package config

import (
	"strings"
	"testing"
)

func TestLoadPositiveIntEnv_Default(t *testing.T) {
	v, err := LoadPositiveIntEnv("NONEXISTENT_VAR_XYZ", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 42 {
		t.Fatalf("expected default 42, got %d", v)
	}
}

func TestLoadPositiveIntEnv_EnvSet(t *testing.T) {
	t.Setenv("TEST_LOAD_INT", "7")
	v, err := LoadPositiveIntEnv("TEST_LOAD_INT", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 7 {
		t.Fatalf("expected 7, got %d", v)
	}
}

func TestLoadPositiveIntEnv_Invalid(t *testing.T) {
	t.Setenv("TEST_LOAD_BAD", "abc")
	_, err := LoadPositiveIntEnv("TEST_LOAD_BAD", 42)
	if err == nil {
		t.Fatal("expected error for non-int value")
	}
}

func TestLoadPositiveIntEnv_Zero(t *testing.T) {
	t.Setenv("TEST_LOAD_ZERO", "0")
	_, err := LoadPositiveIntEnv("TEST_LOAD_ZERO", 42)
	if err == nil || !strings.Contains(err.Error(), "must be >= 1") {
		t.Fatalf("expected 'must be >= 1' error, got %v", err)
	}
}

func TestLoadPositiveIntEnv_Negative(t *testing.T) {
	t.Setenv("TEST_LOAD_NEG", "-5")
	_, err := LoadPositiveIntEnv("TEST_LOAD_NEG", 42)
	if err == nil || !strings.Contains(err.Error(), "must be >= 1") {
		t.Fatalf("expected 'must be >= 1' error, got %v", err)
	}
}

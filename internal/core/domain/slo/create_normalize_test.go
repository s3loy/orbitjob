package slo

import (
	"strings"
	"testing"
	"time"

	"orbitjob/internal/domain/validation"
)

func TestNormalizeCreate_Valid(t *testing.T) {
	spec, err := NormalizeCreate(CreateInput{
		Name:           "api-uptime-slo",
		SLIID:          1,
		Target:         0.999,
		WindowType:     WindowTypeRolling,
		WindowDuration: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Name != "api-uptime-slo" {
		t.Errorf("name = %q, want %q", spec.Name, "api-uptime-slo")
	}
	if spec.SLIID != 1 {
		t.Errorf("sli_id = %d, want %d", spec.SLIID, 1)
	}
	if spec.Target != 0.999 {
		t.Errorf("target = %f, want %f", spec.Target, 0.999)
	}
	if spec.WindowType != WindowTypeRolling {
		t.Errorf("window_type = %q, want %q", spec.WindowType, WindowTypeRolling)
	}
	if spec.WindowDuration != 30*24*time.Hour {
		t.Errorf("window_duration = %v, want %v", spec.WindowDuration, 30*24*time.Hour)
	}
}

func TestNormalizeCreate_MissingName(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:           "   ",
		SLIID:          1,
		Target:         0.999,
		WindowType:     WindowTypeRolling,
		WindowDuration: 30 * 24 * time.Hour,
	})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "name" {
		t.Errorf("field = %q, want %q", vErr.Field, "name")
	}
}

func TestNormalizeCreate_InvalidSLIID(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:           "test",
		SLIID:          0,
		Target:         0.999,
		WindowType:     WindowTypeRolling,
		WindowDuration: 30 * 24 * time.Hour,
	})
	if err == nil {
		t.Fatal("expected error for invalid sli_id")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "sli_id" {
		t.Errorf("field = %q, want %q", vErr.Field, "sli_id")
	}
}

func TestNormalizeCreate_TargetOutOfRange(t *testing.T) {
	tests := []struct {
		name   string
		target float64
	}{
		{"zero", 0},
		{"too_small", 0.00001},
		{"above_one", 1.0001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeCreate(CreateInput{
				Name:           "test",
				SLIID:          1,
				Target:         tt.target,
				WindowType:     WindowTypeRolling,
				WindowDuration: 30 * 24 * time.Hour,
			})
			if err == nil {
				t.Fatal("expected error for target out of range")
			}
			if !validation.Is(err) {
				t.Fatalf("expected validation error, got %T", err)
			}
			var vErr *validation.Error
			if !validation.As(err, &vErr) {
				t.Fatal("expected error to unwrap as validation.Error")
			}
			if vErr.Field != "target" {
				t.Errorf("field = %q, want %q", vErr.Field, "target")
			}
		})
	}
}

func TestNormalizeCreate_TargetBoundary(t *testing.T) {
	// MinTarget + epsilon should work
	_, err := NormalizeCreate(CreateInput{
		Name:           "test",
		SLIID:          1,
		Target:         MinTarget + 0.0001,
		WindowType:     WindowTypeRolling,
		WindowDuration: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("unexpected error for target just above min: %v", err)
	}

	// Exactly MaxTarget should work
	_, err = NormalizeCreate(CreateInput{
		Name:           "test",
		SLIID:          1,
		Target:         MaxTarget,
		WindowType:     WindowTypeRolling,
		WindowDuration: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("unexpected error for target at max: %v", err)
	}
}

func TestNormalizeCreate_InvalidWindowType(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:           "test",
		SLIID:          1,
		Target:         0.999,
		WindowType:     "invalid",
		WindowDuration: 30 * 24 * time.Hour,
	})
	if err == nil {
		t.Fatal("expected error for invalid window_type")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "window_type" {
		t.Errorf("field = %q, want %q", vErr.Field, "window_type")
	}
}

func TestNormalizeCreate_NegativeWindowDuration(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:           "test",
		SLIID:          1,
		Target:         0.999,
		WindowType:     WindowTypeRolling,
		WindowDuration: -1 * time.Hour,
	})
	if err == nil {
		t.Fatal("expected error for negative window_duration")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "window_duration" {
		t.Errorf("field = %q, want %q", vErr.Field, "window_duration")
	}
}

func TestNormalizeCreate_ZeroWindowDuration(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:           "test",
		SLIID:          1,
		Target:         0.999,
		WindowType:     WindowTypeRolling,
		WindowDuration: 0,
	})
	if err == nil {
		t.Fatal("expected error for zero window_duration")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
}

func TestNormalizeCreate_SlowBurnGreaterThanFastBurn(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:              "test",
		SLIID:             1,
		Target:            0.999,
		WindowType:        WindowTypeRolling,
		WindowDuration:    30 * 24 * time.Hour,
		AlertFastBurnRate: 5.0,
		AlertSlowBurnRate: 10.0,
	})
	if err == nil {
		t.Fatal("expected error when slow_burn >= fast_burn")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "alert_slow_burn_rate" {
		t.Errorf("field = %q, want %q", vErr.Field, "alert_slow_burn_rate")
	}
}

func TestNormalizeCreate_SlowBurnEqualToFastBurn(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:              "test",
		SLIID:             1,
		Target:            0.999,
		WindowType:        WindowTypeRolling,
		WindowDuration:    30 * 24 * time.Hour,
		AlertFastBurnRate: 5.0,
		AlertSlowBurnRate: 5.0,
	})
	if err == nil {
		t.Fatal("expected error when slow_burn == fast_burn")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
}

func TestNormalizeCreate_DefaultBurnRates(t *testing.T) {
	spec, err := NormalizeCreate(CreateInput{
		Name:           "test",
		SLIID:          1,
		Target:         0.999,
		WindowType:     WindowTypeRolling,
		WindowDuration: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.AlertFastBurnRate != DefaultFastBurnRate {
		t.Errorf("default fast_burn = %f, want %f", spec.AlertFastBurnRate, DefaultFastBurnRate)
	}
	if spec.AlertSlowBurnRate != DefaultSlowBurnRate {
		t.Errorf("default slow_burn = %f, want %f", spec.AlertSlowBurnRate, DefaultSlowBurnRate)
	}
}

func TestNormalizeCreate_CustomBurnRates(t *testing.T) {
	spec, err := NormalizeCreate(CreateInput{
		Name:              "test",
		SLIID:             1,
		Target:            0.999,
		WindowType:        WindowTypeRolling,
		WindowDuration:    30 * 24 * time.Hour,
		AlertFastBurnRate: 20.0,
		AlertSlowBurnRate: 3.0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.AlertFastBurnRate != 20.0 {
		t.Errorf("fast_burn = %f, want %f", spec.AlertFastBurnRate, 20.0)
	}
	if spec.AlertSlowBurnRate != 3.0 {
		t.Errorf("slow_burn = %f, want %f", spec.AlertSlowBurnRate, 3.0)
	}
}

func TestNormalizeCreate_DefaultWindowType(t *testing.T) {
	spec, err := NormalizeCreate(CreateInput{
		Name:           "test",
		SLIID:          1,
		Target:         0.999,
		WindowDuration: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.WindowType != WindowTypeRolling {
		t.Errorf("default window_type = %q, want %q", spec.WindowType, WindowTypeRolling)
	}
}

func TestNormalizeCreate_NameTooLong(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:           strings.Repeat("a", MaxNameLength+1),
		SLIID:          1,
		Target:         0.999,
		WindowType:     WindowTypeRolling,
		WindowDuration: 30 * 24 * time.Hour,
	})
	if err == nil {
		t.Fatal("expected error for name too long")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "name" {
		t.Errorf("field = %q, want %q", vErr.Field, "name")
	}
}

func TestNormalizeCreate_NegativeSLIID(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:           "test",
		SLIID:          -1,
		Target:         0.999,
		WindowType:     WindowTypeRolling,
		WindowDuration: 30 * 24 * time.Hour,
	})
	if err == nil {
		t.Fatal("expected error for negative sli_id")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "sli_id" {
		t.Errorf("field = %q, want %q", vErr.Field, "sli_id")
	}
}

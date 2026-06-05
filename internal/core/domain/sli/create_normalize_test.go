package sli

import (
	"strings"
	"testing"

	"orbitjob/internal/domain/validation"
)

func TestNormalizeCreate_Valid(t *testing.T) {
	checkID := float64(42)
	spec, err := NormalizeCreate(CreateInput{
		Name:              "api-availability",
		SLIType:           TypeAvailability,
		SourceType:        SourceTypeCheckRun,
		SourceConfig:      map[string]any{"check_id": checkID},
		Aggregation:       AggregationRatio,
		GoodEventCriteria: map[string]any{"status": "up"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Name != "api-availability" {
		t.Errorf("name = %q, want %q", spec.Name, "api-availability")
	}
	if spec.SLIType != TypeAvailability {
		t.Errorf("sli_type = %q, want %q", spec.SLIType, TypeAvailability)
	}
	if spec.SourceType != SourceTypeCheckRun {
		t.Errorf("source_type = %q, want %q", spec.SourceType, SourceTypeCheckRun)
	}
	if spec.Aggregation != AggregationRatio {
		t.Errorf("aggregation = %q, want %q", spec.Aggregation, AggregationRatio)
	}
}

func TestNormalizeCreate_MissingName(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:              "   ",
		SLIType:           TypeAvailability,
		SourceType:        SourceTypeCheckRun,
		SourceConfig:      map[string]any{"check_id": float64(1)},
		GoodEventCriteria: map[string]any{"status": "up"},
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

func TestNormalizeCreate_InvalidSLIType(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:              "test",
		SLIType:           "invalid_type",
		SourceType:        SourceTypeCheckRun,
		SourceConfig:      map[string]any{"check_id": float64(1)},
		GoodEventCriteria: map[string]any{"status": "up"},
	})
	if err == nil {
		t.Fatal("expected error for invalid sli_type")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "sli_type" {
		t.Errorf("field = %q, want %q", vErr.Field, "sli_type")
	}
}

func TestNormalizeCreate_MissingCheckID(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:              "test",
		SLIType:           TypeAvailability,
		SourceType:        SourceTypeCheckRun,
		SourceConfig:      map[string]any{},
		GoodEventCriteria: map[string]any{"status": "up"},
	})
	if err == nil {
		t.Fatal("expected error for missing check_id")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "source_config.check_id" {
		t.Errorf("field = %q, want %q", vErr.Field, "source_config.check_id")
	}
}

func TestNormalizeCreate_MissingGoodEventCriteria(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:         "test",
		SLIType:      TypeAvailability,
		SourceType:   SourceTypeCheckRun,
		SourceConfig: map[string]any{"check_id": float64(1)},
	})
	if err == nil {
		t.Fatal("expected error for missing good_event_criteria")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "good_event_criteria" {
		t.Errorf("field = %q, want %q", vErr.Field, "good_event_criteria")
	}
}

func TestNormalizeCreate_Defaults(t *testing.T) {
	spec, err := NormalizeCreate(CreateInput{
		Name:              "test",
		SourceConfig:      map[string]any{"check_id": float64(1)},
		GoodEventCriteria: map[string]any{"status": "up"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.SLIType != TypeAvailability {
		t.Errorf("default sli_type = %q, want %q", spec.SLIType, TypeAvailability)
	}
	if spec.SourceType != SourceTypeCheckRun {
		t.Errorf("default source_type = %q, want %q", spec.SourceType, SourceTypeCheckRun)
	}
	if spec.Aggregation != AggregationRatio {
		t.Errorf("default aggregation = %q, want %q", spec.Aggregation, AggregationRatio)
	}
}

func TestNormalizeCreate_NameTooLong(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:              strings.Repeat("a", MaxNameLength+1),
		SLIType:           TypeAvailability,
		SourceType:        SourceTypeCheckRun,
		SourceConfig:      map[string]any{"check_id": float64(1)},
		GoodEventCriteria: map[string]any{"status": "up"},
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

func TestNormalizeCreate_InvalidSourceType(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:              "test",
		SLIType:           TypeAvailability,
		SourceType:        "invalid_source",
		SourceConfig:      map[string]any{"check_id": float64(1)},
		GoodEventCriteria: map[string]any{"status": "up"},
	})
	if err == nil {
		t.Fatal("expected error for invalid source_type")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "source_type" {
		t.Errorf("field = %q, want %q", vErr.Field, "source_type")
	}
}

func TestNormalizeCreate_InvalidAggregation(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:              "test",
		SLIType:           TypeAvailability,
		SourceType:        SourceTypeCheckRun,
		SourceConfig:      map[string]any{"check_id": float64(1)},
		Aggregation:       "invalid_agg",
		GoodEventCriteria: map[string]any{"status": "up"},
	})
	if err == nil {
		t.Fatal("expected error for invalid aggregation")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "aggregation" {
		t.Errorf("field = %q, want %q", vErr.Field, "aggregation")
	}
}

func TestNormalizeCreate_CheckIDTypes(t *testing.T) {
	base := CreateInput{
		Name:              "test",
		SLIType:           TypeAvailability,
		SourceType:        SourceTypeCheckRun,
		GoodEventCriteria: map[string]any{"status": "up"},
	}

	tests := []struct {
		name    string
		checkID any
		wantErr bool
	}{
		{"float64", float64(42), false},
		{"int64", int64(42), false},
		{"int", int(42), false},
		{"string", "42", true},
		{"nil", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := base
			in.SourceConfig = map[string]any{"check_id": tt.checkID}
			_, err := NormalizeCreate(in)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !validation.Is(err) {
					t.Fatalf("expected validation error, got %T", err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestNormalizeCreate_NilSourceConfig(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:              "test",
		SLIType:           TypeAvailability,
		SourceType:        SourceTypeCheckRun,
		GoodEventCriteria: map[string]any{"status": "up"},
	})
	if err == nil {
		t.Fatal("expected error for nil source_config")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "source_config" {
		t.Errorf("field = %q, want %q", vErr.Field, "source_config")
	}
}

func TestNormalizeCreate_LatencyDoesNotRequireGoodEventCriteria(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:         "test",
		SLIType:      TypeLatency,
		SourceType:   SourceTypeCheckRun,
		SourceConfig: map[string]any{"check_id": float64(1)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeCreate_CustomDoesNotRequireGoodEventCriteria(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:         "test",
		SLIType:      TypeCustom,
		SourceType:   SourceTypeCheckRun,
		SourceConfig: map[string]any{"check_id": float64(1)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeCreate_QualityRequiresGoodEventCriteria(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:         "test",
		SLIType:      TypeQuality,
		SourceType:   SourceTypeCheckRun,
		SourceConfig: map[string]any{"check_id": float64(1)},
	})
	if err == nil {
		t.Fatal("expected error for missing good_event_criteria on quality type")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "good_event_criteria" {
		t.Errorf("field = %q, want %q", vErr.Field, "good_event_criteria")
	}
}

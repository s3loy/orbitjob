package sli

import (
	"strings"
	"testing"

	"orbitjob/internal/domain/validation"
)

func TestNormalizeCreate_Valid(t *testing.T) {
	spec, err := NormalizeCreate(CreateInput{
		Name:              "api-availability",
		SLIType:           TypeAvailability,
		SourceType:        SourceTypeJobRun,
		SourceConfig:      map[string]any{"source_uid": "check-42"},
		Aggregation:       AggregationRatio,
		GoodEventCriteria: map[string]any{"status": "success"},
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
	if spec.SourceType != SourceTypeJobRun {
		t.Errorf("source_type = %q, want %q", spec.SourceType, SourceTypeJobRun)
	}
	if spec.Aggregation != AggregationRatio {
		t.Errorf("aggregation = %q, want %q", spec.Aggregation, AggregationRatio)
	}
}

func TestNormalizeCreate_MissingName(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:              "   ",
		SLIType:           TypeAvailability,
		SourceType:        SourceTypeJobRun,
		SourceConfig:      map[string]any{"source_uid": "check-1"},
		GoodEventCriteria: map[string]any{"status": "success"},
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
		SourceType:        SourceTypeJobRun,
		SourceConfig:      map[string]any{"source_uid": "check-1"},
		GoodEventCriteria: map[string]any{"status": "success"},
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

func TestNormalizeCreate_MissingSourceUID(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:              "test",
		SLIType:           TypeAvailability,
		SourceType:        SourceTypeJobRun,
		SourceConfig:      map[string]any{},
		GoodEventCriteria: map[string]any{"status": "success"},
	})
	if err == nil {
		t.Fatal("expected error for missing source_uid")
	}
	if !validation.Is(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
	var vErr *validation.Error
	if !validation.As(err, &vErr) {
		t.Fatal("expected error to unwrap as validation.Error")
	}
	if vErr.Field != "source_config.source_uid" {
		t.Errorf("field = %q, want %q", vErr.Field, "source_config.source_uid")
	}
}

func TestNormalizeCreate_MissingGoodEventCriteria(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:         "test",
		SLIType:      TypeAvailability,
		SourceType:   SourceTypeJobRun,
		SourceConfig: map[string]any{"source_uid": "check-1"},
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
		SourceConfig:      map[string]any{"source_uid": "check-1"},
		GoodEventCriteria: map[string]any{"status": "success"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.SLIType != TypeAvailability {
		t.Errorf("default sli_type = %q, want %q", spec.SLIType, TypeAvailability)
	}
	if spec.SourceType != SourceTypeJobRun {
		t.Errorf("default source_type = %q, want %q", spec.SourceType, SourceTypeJobRun)
	}
	if spec.Aggregation != AggregationRatio {
		t.Errorf("default aggregation = %q, want %q", spec.Aggregation, AggregationRatio)
	}
}

func TestNormalizeCreate_NameTooLong(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:              strings.Repeat("a", MaxNameLength+1),
		SLIType:           TypeAvailability,
		SourceType:        SourceTypeJobRun,
		SourceConfig:      map[string]any{"source_uid": "check-1"},
		GoodEventCriteria: map[string]any{"status": "success"},
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
		SourceType:        "check_run",
		SourceConfig:      map[string]any{"source_uid": "check-1"},
		GoodEventCriteria: map[string]any{"status": "success"},
	})
	if err == nil {
		t.Fatal("expected error for the retired check_run source_type")
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
		SourceType:        SourceTypeJobRun,
		SourceConfig:      map[string]any{"source_uid": "check-1"},
		Aggregation:       "invalid_agg",
		GoodEventCriteria: map[string]any{"status": "success"},
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

func TestNormalizeCreate_SourceUIDShapes(t *testing.T) {
	base := CreateInput{
		Name:              "test",
		SLIType:           TypeAvailability,
		SourceType:        SourceTypeJobRun,
		GoodEventCriteria: map[string]any{"status": "success"},
	}

	tests := []struct {
		name      string
		sourceUID any
		wantErr   bool
	}{
		{"check source", "check-42", false},
		{"scheduled job source", "018f3a2b-7c1d-7c3e-9f4a-b2d1e0c8a910", false},
		{"string", 42, true},
		{"nil", nil, true},
		{"blank", "   ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := base
			in.SourceConfig = map[string]any{"source_uid": tt.sourceUID}
			_, err := NormalizeCreate(in)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestNormalizeCreate_NilSourceConfig(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:              "test",
		SLIType:           TypeAvailability,
		SourceType:        SourceTypeJobRun,
		GoodEventCriteria: map[string]any{"status": "success"},
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
		SourceType:   SourceTypeJobRun,
		SourceConfig: map[string]any{"source_uid": "check-1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeCreate_CustomDoesNotRequireGoodEventCriteria(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:         "test",
		SLIType:      TypeCustom,
		SourceType:   SourceTypeJobRun,
		SourceConfig: map[string]any{"source_uid": "check-1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeCreate_QualityRequiresGoodEventCriteria(t *testing.T) {
	_, err := NormalizeCreate(CreateInput{
		Name:         "test",
		SLIType:      TypeQuality,
		SourceType:   SourceTypeJobRun,
		SourceConfig: map[string]any{"source_uid": "check-1"},
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

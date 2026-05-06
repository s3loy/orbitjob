package job

import (
	"strings"
	"testing"
	"time"

	"orbitjob/internal/domain/validation"
)

func TestNormalizeUpdate_ManualJobClearsCron(t *testing.T) {
	now := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)

	spec, err := NormalizeUpdate(now, UpdateInput{
		ID:          42,
		Version:     4,
		Name:        "manual-report",
		TenantID:    "tenant-a",
		TriggerType: TriggerTypeManual,
		HandlerType: "http",
	})
	if err != nil {
		t.Fatalf("NormalizeUpdate() error = %v", err)
	}
	if spec.CronExpr != nil {
		t.Fatalf("manual job should clear cron_expr")
	}
	if spec.NextRunAt != nil {
		t.Fatalf("manual job should clear next_run_at")
	}
	if spec.Version != 4 {
		t.Fatalf("expected version=%d, got %d", 4, spec.Version)
	}
}

func TestNormalizeUpdate_CronJobComputesNextRunAt(t *testing.T) {
	now := time.Date(2026, 4, 7, 0, 58, 0, 0, time.UTC)
	cronExpr := "0 9 * * *"

	spec, err := NormalizeUpdate(now, UpdateInput{
		ID:          7,
		Version:     2,
		Name:        "daily-report",
		TenantID:    "tenant-a",
		TriggerType: TriggerTypeCron,
		CronExpr:    &cronExpr,
		Timezone:    "Asia/Shanghai",
		HandlerType: "http",
	})
	if err != nil {
		t.Fatalf("NormalizeUpdate() error = %v", err)
	}
	if spec.NextRunAt == nil {
		t.Fatalf("expected next_run_at to be computed")
	}

	wantNextRunAt := time.Date(2026, 4, 7, 1, 0, 0, 0, time.UTC)
	if !spec.NextRunAt.Equal(wantNextRunAt) {
		t.Fatalf("expected next_run_at=%s, got %s",
			wantNextRunAt.Format(time.RFC3339),
			spec.NextRunAt.Format(time.RFC3339),
		)
	}
}

func TestNormalizeUpdate_TracksRoutingFields(t *testing.T) {
	now := time.Date(2026, 4, 7, 0, 58, 0, 0, time.UTC)
	partitionKey := " shard-b "

	spec, err := NormalizeUpdate(now, UpdateInput{
		ID:           7,
		Version:      2,
		Name:         "daily-report",
		TenantID:     "tenant-a",
		Priority:     12,
		PartitionKey: &partitionKey,
		TriggerType:  TriggerTypeManual,
		HandlerType:  "http",
	})
	if err != nil {
		t.Fatalf("NormalizeUpdate() error = %v", err)
	}
	if spec.Priority != 12 {
		t.Fatalf("expected priority=%d, got %d", 12, spec.Priority)
	}
	if spec.PartitionKey == nil || *spec.PartitionKey != "shard-b" {
		t.Fatalf("expected partition_key=%q, got %+v", "shard-b", spec.PartitionKey)
	}
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkNormalizeUpdate(b *testing.B) {
	now := time.Date(2026, 4, 7, 0, 58, 0, 0, time.UTC)
	cronExpr := "0 9 * * *"

	tests := []struct {
		name  string
		input UpdateInput
	}{
		{"manual_minimal", UpdateInput{
			ID: 42, Version: 3, Name: "manual-report", TriggerType: TriggerTypeManual, HandlerType: "http",
		}},
		{"cron_full", UpdateInput{
			ID: 7, Version: 2, Name: "daily-report", TenantID: "tenant-a", Priority: 9,
			TriggerType: TriggerTypeCron, CronExpr: &cronExpr, Timezone: "Asia/Shanghai",
			HandlerType: "http", TimeoutSec: 300,
		}},
		{"validation_error", UpdateInput{ID: 0, Version: 0}},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = NormalizeUpdate(now, tt.input)
			}
		})
	}
}

func TestNormalizeUpdate_InvalidInputReturnsValidationError(t *testing.T) {
	tests := []struct {
		name        string
		input       UpdateInput
		wantField   string
		wantMessage string
	}{
		{
			name: "id less than one",
			input: UpdateInput{
				ID:      0,
				Version: 1,
			},
			wantField:   "id",
			wantMessage: "must be >= 1",
		},
		{
			name: "version less than one",
			input: UpdateInput{
				ID:      1,
				Version: 0,
			},
			wantField:   "version",
			wantMessage: "must be >= 1",
		},
		{
			name: "priority less than zero",
			input: UpdateInput{
				ID:          1,
				Version:     1,
				Name:        "demo",
				TenantID:    "tenant-a",
				Priority:    -1,
				TriggerType: TriggerTypeManual,
				HandlerType: "http",
			},
			wantField:   "priority",
			wantMessage: "must be >= 0",
		},
		{
			name: "tenant too long",
			input: UpdateInput{
				ID:          1,
				Version:     1,
				Name:        "demo",
				TenantID:    strings.Repeat("t", 65),
				TriggerType: TriggerTypeManual,
				HandlerType: "http",
			},
			wantField:   "tenant_id",
			wantMessage: "must be <= 64 characters",
		},
		{
			name: "partition key too long",
			input: UpdateInput{
				ID:          1,
				Version:     1,
				Name:        "demo",
				TenantID:    "tenant-a",
				TriggerType: TriggerTypeManual,
				HandlerType: "http",
				PartitionKey: func() *string {
					value := strings.Repeat("p", 65)
					return &value
				}(),
			},
			wantField:   "partition_key",
			wantMessage: "must be <= 64 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeUpdate(time.Now().UTC(), tt.input)
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !validation.Is(err) {
				t.Fatalf("expected validation error, got %T", err)
			}

			var validationErr *ValidationError
			if !validation.As(err, &validationErr) {
				t.Fatalf("expected error to unwrap as ValidationError")
			}
			if validationErr.Field != tt.wantField {
				t.Fatalf("expected field=%q, got %q", tt.wantField, validationErr.Field)
			}
			if validationErr.Message != tt.wantMessage {
				t.Fatalf("expected message=%q, got %q", tt.wantMessage, validationErr.Message)
			}
		})
	}
}

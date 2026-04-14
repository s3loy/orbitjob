package instance

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeCreate_DefaultsAndTrim(t *testing.T) {
	partitionKey := " shard-a "
	idempotencyKey := " request-1 "
	routingKey := " route-a "
	traceID := " trace-1 "

	spec, err := NormalizeCreate(CreateInput{
		JobID:          42,
		ScheduledAt:    time.Date(2026, 4, 13, 1, 2, 3, 0, time.FixedZone("UTC+8", 8*3600)),
		Priority:       8,
		PartitionKey:   &partitionKey,
		IdempotencyKey: &idempotencyKey,
		RoutingKey:     &routingKey,
		TraceID:        &traceID,
	})
	if err != nil {
		t.Fatalf("NormalizeCreate() error = %v", err)
	}
	if spec.TenantID != DefaultTenantID {
		t.Fatalf("expected tenant_id=%q, got %q", DefaultTenantID, spec.TenantID)
	}
	if spec.TriggerSource != TriggerSourceSchedule {
		t.Fatalf("expected trigger_source=%q, got %q", TriggerSourceSchedule, spec.TriggerSource)
	}
	if spec.MaxAttempt != 1 {
		t.Fatalf("expected max_attempt=1, got %d", spec.MaxAttempt)
	}
	if spec.PartitionKey == nil || *spec.PartitionKey != "shard-a" {
		t.Fatalf("expected partition_key=%q, got %+v", "shard-a", spec.PartitionKey)
	}
	if spec.IdempotencyKey == nil || *spec.IdempotencyKey != "request-1" {
		t.Fatalf("expected idempotency_key=%q, got %+v", "request-1", spec.IdempotencyKey)
	}
	if spec.RoutingKey == nil || *spec.RoutingKey != "route-a" {
		t.Fatalf("expected routing_key=%q, got %+v", "route-a", spec.RoutingKey)
	}
	if spec.TraceID == nil || *spec.TraceID != "trace-1" {
		t.Fatalf("expected trace_id=%q, got %+v", "trace-1", spec.TraceID)
	}
	if spec.ScheduledAt.Location() != time.UTC {
		t.Fatalf("expected scheduled_at to be normalized to UTC")
	}
}

func TestNormalizeCreate_InvalidInput(t *testing.T) {
	tests := []struct {
		name        string
		input       CreateInput
		wantField   string
		wantMessage string
	}{
		{
			name: "job id less than one",
			input: CreateInput{
				ScheduledAt: time.Now().UTC(),
			},
			wantField:   "job_id",
			wantMessage: "must be >= 1",
		},
		{
			name: "negative priority",
			input: CreateInput{
				JobID:       1,
				ScheduledAt: time.Now().UTC(),
				Priority:    -1,
			},
			wantField:   "priority",
			wantMessage: "must be >= 0",
		},
		{
			name: "partition key too long",
			input: CreateInput{
				JobID:       1,
				ScheduledAt: time.Now().UTC(),
				PartitionKey: func() *string {
					value := strings.Repeat("p", 65)
					return &value
				}(),
			},
			wantField:   "partition_key",
			wantMessage: "must be <= 64 characters",
		},
		{
			name: "missing scheduled at",
			input: CreateInput{
				JobID: 1,
			},
			wantField:   "scheduled_at",
			wantMessage: "is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeCreate(tt.input)
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			var validationErr *ValidationError
			if !AsValidationError(err, &validationErr) {
				t.Fatalf("expected ValidationError, got %T", err)
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

package instance

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeStart_DefaultsAndUTC(t *testing.T) {
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))

	spec, err := NormalizeStart(StartInput{
		InstanceID: 42,
		WorkerID:   " worker-a ",
		Now:        now,
	})
	if err != nil {
		t.Fatalf("NormalizeStart() error = %v", err)
	}
	if spec.TenantID != DefaultTenantID {
		t.Fatalf("expected tenant_id=%q, got %q", DefaultTenantID, spec.TenantID)
	}
	if spec.InstanceID != 42 {
		t.Fatalf("expected instance_id=42, got %d", spec.InstanceID)
	}
	if spec.WorkerID != "worker-a" {
		t.Fatalf("expected worker_id=%q, got %q", "worker-a", spec.WorkerID)
	}
	if spec.StartedAt.Location() != time.UTC {
		t.Fatalf("expected started_at in UTC, got %v", spec.StartedAt.Location())
	}
	if !spec.StartedAt.Equal(now.UTC()) {
		t.Fatalf("expected started_at=%v, got %v", now.UTC(), spec.StartedAt)
	}
}

func TestNormalizeStart_InvalidInput(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name        string
		input       StartInput
		wantField   string
		wantMessage string
	}{
		{
			name: "instance id less than one",
			input: StartInput{
				WorkerID: "worker-a",
				Now:      now,
			},
			wantField:   "instance_id",
			wantMessage: "must be >= 1",
		},
		{
			name: "missing worker id",
			input: StartInput{
				InstanceID: 1,
				Now:        now,
			},
			wantField:   "worker_id",
			wantMessage: "is required",
		},
		{
			name: "worker id too long",
			input: StartInput{
				InstanceID: 1,
				WorkerID:   strings.Repeat("w", 65),
				Now:        now,
			},
			wantField:   "worker_id",
			wantMessage: "must be <= 64 characters",
		},
		{
			name: "missing now",
			input: StartInput{
				InstanceID: 1,
				WorkerID:   "worker-a",
			},
			wantField:   "now",
			wantMessage: "is required",
		},
		{
			name: "tenant id too long",
			input: StartInput{
				TenantID:   strings.Repeat("t", 65),
				InstanceID: 1,
				WorkerID:   "worker-a",
				Now:        now,
			},
			wantField:   "tenant_id",
			wantMessage: "must be <= 64 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeStart(tt.input)
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

package worker

import (
	"strings"
	"testing"
	"time"

	"orbitjob/internal/domain/validation"
)

func TestNormalizeHeartbeat_Defaults(t *testing.T) {
	now := time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC)

	spec, err := NormalizeHeartbeat(now, HeartbeatInput{
		WorkerID:       "worker-a",
		LeaseExpiresAt: now.Add(30 * time.Second),
		Labels:         map[string]any{"queue": "video"},
	})
	if err != nil {
		t.Fatalf("NormalizeHeartbeat() error = %v", err)
	}
	if spec.TenantID != DefaultTenantID {
		t.Fatalf("expected tenant_id=%q, got %q", DefaultTenantID, spec.TenantID)
	}
	if spec.Status != StatusOnline {
		t.Fatalf("expected status=%q, got %q", StatusOnline, spec.Status)
	}
	if spec.Capacity != 1 {
		t.Fatalf("expected capacity=1, got %d", spec.Capacity)
	}
	if spec.Labels["queue"] != "video" {
		t.Fatalf("expected labels.queue=%q, got %#v", "video", spec.Labels["queue"])
	}
}

func TestNormalizeHeartbeat_InvalidInput(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name        string
		input       HeartbeatInput
		wantField   string
		wantMessage string
	}{
		{
			name: "missing worker id",
			input: HeartbeatInput{
				LeaseExpiresAt: now.Add(time.Minute),
			},
			wantField:   "worker_id",
			wantMessage: "is required",
		},
		{
			name: "invalid status",
			input: HeartbeatInput{
				WorkerID:       "worker-a",
				Status:         "busy",
				LeaseExpiresAt: now.Add(time.Minute),
			},
			wantField:   "status",
			wantMessage: "must be one of: online, offline, draining",
		},
		{
			name: "capacity less than one",
			input: HeartbeatInput{
				WorkerID:       "worker-a",
				Capacity:       -1,
				LeaseExpiresAt: now.Add(time.Minute),
			},
			wantField:   "capacity",
			wantMessage: "must be >= 1",
		},
		{
			name: "lease not after heartbeat",
			input: HeartbeatInput{
				WorkerID:       "worker-a",
				LeaseExpiresAt: now,
			},
			wantField:   "lease_expires_at",
			wantMessage: "must be after last_heartbeat_at",
		},
		{
			name: "tenant too long",
			input: HeartbeatInput{
				TenantID:       strings.Repeat("t", 65),
				WorkerID:       "worker-a",
				LeaseExpiresAt: now.Add(time.Minute),
			},
			wantField:   "tenant_id",
			wantMessage: "must be <= 64 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeHeartbeat(now, tt.input)
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			var validationErr *ValidationError
			if !validation.As(err, &validationErr) {
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

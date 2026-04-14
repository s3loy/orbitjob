package instance

import (
	"testing"
	"time"
)

func TestNormalizeClaim_DefaultTenant(t *testing.T) {
	now := time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC)
	leaseExpiresAt := now.Add(30 * time.Second)

	spec, err := NormalizeClaim(ClaimInput{
		WorkerID:       "worker-a",
		Now:            now,
		LeaseExpiresAt: leaseExpiresAt,
	})
	if err != nil {
		t.Fatalf("NormalizeClaim() error = %v", err)
	}
	if spec.TenantID != DefaultTenantID {
		t.Fatalf("expected tenant_id=%q, got %q", DefaultTenantID, spec.TenantID)
	}
	if spec.WorkerID != "worker-a" {
		t.Fatalf("expected worker_id=%q, got %q", "worker-a", spec.WorkerID)
	}
}

func TestNormalizeClaim_InvalidInput(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name        string
		input       ClaimInput
		wantField   string
		wantMessage string
	}{
		{
			name: "missing worker id",
			input: ClaimInput{
				Now:            now,
				LeaseExpiresAt: now.Add(time.Minute),
			},
			wantField:   "worker_id",
			wantMessage: "is required",
		},
		{
			name: "missing now",
			input: ClaimInput{
				WorkerID:       "worker-a",
				LeaseExpiresAt: now.Add(time.Minute),
			},
			wantField:   "now",
			wantMessage: "is required",
		},
		{
			name: "lease not after now",
			input: ClaimInput{
				WorkerID:       "worker-a",
				Now:            now,
				LeaseExpiresAt: now,
			},
			wantField:   "lease_expires_at",
			wantMessage: "must be after now",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeClaim(tt.input)
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

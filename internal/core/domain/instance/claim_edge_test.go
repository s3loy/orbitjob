package instance

import (
	"strings"
	"testing"
	"time"

	"orbitjob/internal/domain/validation"
)

func TestNormalizeClaim_LeaseExpiresAtZero(t *testing.T) {
	now := time.Now().UTC()
	_, err := NormalizeClaim(ClaimInput{
		Now: now,
	})
	if err == nil {
		t.Fatal("expected validation error for zero lease_expires_at, got nil")
	}
	var validationErr *ValidationError
	if !validation.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if validationErr.Field != "lease_expires_at" {
		t.Fatalf("expected field=lease_expires_at, got %q", validationErr.Field)
	}
}

func TestNormalizeClaim_TenantIDTooLong(t *testing.T) {
	now := time.Now().UTC()
	_, err := NormalizeClaim(ClaimInput{
		TenantID:       strings.Repeat("t", 65),
		Now:            now,
		LeaseExpiresAt: now.Add(time.Minute),
	})
	if err == nil {
		t.Fatal("expected validation error for long tenant_id, got nil")
	}
}

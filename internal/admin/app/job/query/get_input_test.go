package query

import (
	"strings"
	"testing"

	domainjob "orbitjob/internal/core/domain/job"
)

func TestNormalizeGetInput_DefaultTenant(t *testing.T) {
	out, err := NormalizeGetInput(GetInput{
		ID: 42,
	})
	if err != nil {
		t.Fatalf("NormalizeGetInput() error = %v", err)
	}

	if out.ID != 42 {
		t.Fatalf("expected id=%d, got %d", 42, out.ID)
	}
	if out.TenantID != defaultTenantID {
		t.Fatalf("expected tenant_id=%q, got %q", defaultTenantID, out.TenantID)
	}
}

func TestNormalizeGetInput_TrimsTenantID(t *testing.T) {
	out, err := NormalizeGetInput(GetInput{
		ID:       7,
		TenantID: " tenant-a ",
	})
	if err != nil {
		t.Fatalf("NormalizeGetInput() error = %v", err)
	}

	if out.TenantID != "tenant-a" {
		t.Fatalf("expected tenant_id=%q, got %q", "tenant-a", out.TenantID)
	}
}

func TestNormalizeGetInput_InvalidInput(t *testing.T) {
	tests := []struct {
		name        string
		input       GetInput
		wantField   string
		wantMessage string
	}{
		{
			name: "id less than one",
			input: GetInput{
				ID: 0,
			},
			wantField:   "id",
			wantMessage: "must be >= 1",
		},
		{
			name: "tenant too long",
			input: GetInput{
				ID:       1,
				TenantID: strings.Repeat("t", 65),
			},
			wantField:   "tenant_id",
			wantMessage: "must be <= 64 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeGetInput(tt.input)
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !domainjob.IsValidationError(err) {
				t.Fatalf("expected validation error, got %T", err)
			}

			var validationErr *domainjob.ValidationError
			if !domainjob.AsValidationError(err, &validationErr) {
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

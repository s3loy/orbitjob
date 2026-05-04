package tenant

import (
	"strings"
	"testing"

	"orbitjob/internal/domain/validation"
)

func TestNormalizeCreateTenant_Valid(t *testing.T) {
	in, err := NormalizeCreateTenant(CreateTenantInput{Slug: "acme", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.Slug != "acme" {
		t.Fatalf("expected slug=acme, got %q", in.Slug)
	}
	if in.Name != "Acme Corp" {
		t.Fatalf("expected name='Acme Corp', got %q", in.Name)
	}
}

func TestNormalizeCreateTenant_TrimsSpaces(t *testing.T) {
	in, err := NormalizeCreateTenant(CreateTenantInput{Slug: "  acme  ", Name: "  Acme Corp  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.Slug != "acme" {
		t.Fatalf("expected slug=acme, got %q", in.Slug)
	}
	if in.Name != "Acme Corp" {
		t.Fatalf("expected name='Acme Corp', got %q", in.Name)
	}
}

func TestNormalizeCreateTenant_Invalid(t *testing.T) {
	tests := []struct {
		name        string
		slug        string
		tenantName  string
		wantField   string
		wantMessage string
	}{
		{"empty slug", "", "Acme", "slug", "is required"},
		{"slug too long", strings.Repeat("a", 65), "Acme", "slug", "must be <= 64 characters"},
		{"empty name", "acme", "", "name", "is required"},
		{"name too long", "acme", strings.Repeat("a", 129), "name", "must be <= 128 characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeCreateTenant(CreateTenantInput{Slug: tt.slug, Name: tt.tenantName})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			var verr *validation.Error
			if !validation.As(err, &verr) {
				t.Fatalf("expected validation.Error, got %T", err)
			}
			if verr.Field != tt.wantField {
				t.Fatalf("expected field=%q, got %q", tt.wantField, verr.Field)
			}
			if verr.Message != tt.wantMessage {
				t.Fatalf("expected message=%q, got %q", tt.wantMessage, verr.Message)
			}
		})
	}
}

func TestConstants(t *testing.T) {
	if StatusActive != "active" {
		t.Fatalf("expected StatusActive=active, got %q", StatusActive)
	}
	if StatusSuspended != "suspended" {
		t.Fatalf("expected StatusSuspended=suspended, got %q", StatusSuspended)
	}
	if ActorTypeSystem != "system" {
		t.Fatalf("expected ActorTypeSystem=system, got %q", ActorTypeSystem)
	}
	if ActorTypeAPIKey != "api_key" {
		t.Fatalf("expected ActorTypeAPIKey=api_key, got %q", ActorTypeAPIKey)
	}
}

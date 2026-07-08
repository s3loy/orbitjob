package tenant

import (
	"testing"

	"orbitjob/internal/domain/validation"
)

func TestTenantValidate_Valid(t *testing.T) {
	cases := []struct {
		name   string
		tenant Tenant
	}{
		{"active", Tenant{Slug: "acme", Name: "Acme Corp", Status: StatusActive}},
		{"suspended", Tenant{Slug: "acme", Name: "Acme Corp", Status: StatusSuspended}},
		{"empty status defaults to active", Tenant{Slug: "acme", Name: "Acme Corp", Status: ""}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.tenant.Validate(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestTenantValidate_Invalid(t *testing.T) {
	cases := []struct {
		name        string
		tenant      Tenant
		wantField   string
		wantMessage string
	}{
		{"empty slug", Tenant{Slug: "", Name: "Acme"}, "slug", "required"},
		{"empty name", Tenant{Slug: "acme", Name: ""}, "name", "required"},
		{"invalid status", Tenant{Slug: "acme", Name: "Acme", Status: "deleted"}, "status", "must be active or suspended"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.tenant.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var verr *validation.Error
			if !validation.As(err, &verr) {
				t.Fatalf("expected validation.Error, got %T", err)
			}
			if verr.Field != tc.wantField {
				t.Fatalf("expected field=%q, got %q", tc.wantField, verr.Field)
			}
			if verr.Message != tc.wantMessage {
				t.Fatalf("expected message=%q, got %q", tc.wantMessage, verr.Message)
			}
		})
	}
}

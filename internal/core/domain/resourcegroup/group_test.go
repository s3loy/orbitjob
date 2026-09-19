package resourcegroup_test

import (
	"strings"
	"testing"

	"orbitjob/internal/core/domain/resourcegroup"
	"orbitjob/internal/domain/validation"
)

func TestNewAcceptsAWellFormedGroup(t *testing.T) {
	g, err := resourcegroup.New("01ABC", "tenant-a", "ci", "Continuous Integration")
	if err != nil {
		t.Fatalf("valid group rejected: %v", err)
	}
	if g.Slug != "ci" || g.Name != "Continuous Integration" || g.TenantID != "tenant-a" {
		t.Fatalf("unexpected group: %+v", g)
	}
}

// The slug becomes an ARN segment. The grammar splits on ":" and "/", so a slug
// containing either would make the group unaddressable in a policy -- which
// would surface as a permission that never matches, not as a validation error.
func TestNewNormalizesCaseAndTrims(t *testing.T) {
	g, err := resourcegroup.New("01ABC", "tenant-a", "  CI-Prod  ", "  Production  ")
	if err != nil {
		t.Fatalf("valid group rejected: %v", err)
	}
	if g.Slug != "ci-prod" {
		t.Fatalf("expected a lower-cased slug, got %q", g.Slug)
	}
	if g.Name != "Production" {
		t.Fatalf("expected a trimmed name, got %q", g.Name)
	}
}

func TestNewRejectsBadSlugs(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"leading dash": "-ci",
		"colon":        "a:b",
		"slash":        "a/b",
		"space":        "a b",
		"too long":     strings.Repeat("a", 64),
	}
	for name, slug := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := resourcegroup.New("01ABC", "tenant-a", slug, "Name"); err == nil {
				t.Fatalf("slug %q was accepted", slug)
			}
		})
	}
}

func TestNewRejectsEmptyAndOversizedNames(t *testing.T) {
	if _, err := resourcegroup.New("01ABC", "tenant-a", "ci", "   "); err == nil {
		t.Fatal("an empty name was accepted")
	}
	long := strings.Repeat("n", resourcegroup.MaxNameLength+1)
	if _, err := resourcegroup.New("01ABC", "tenant-a", "ci", long); err == nil {
		t.Fatal("an oversized name was accepted")
	}
}

// Errors carry the field so the API can point at the offending input rather
// than returning a generic rejection.
func TestNewReportsTheFailingField(t *testing.T) {
	_, err := resourcegroup.New("01ABC", "tenant-a", "bad slug", "Name")
	var ve *validation.Error
	if !validation.As(err, &ve) {
		t.Fatalf("expected a validation error, got %T", err)
	}
	if ve.Field != "slug" {
		t.Fatalf("expected field slug, got %q", ve.Field)
	}
}

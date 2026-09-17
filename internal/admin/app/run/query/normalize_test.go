package query

import (
	"errors"
	"strings"
	"testing"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// NormalizeGetInput is the read gate for one run's detail and for the cancel
// target lookup, so its bounds decide what a caller can address. These tests
// pin the gate: what it refuses, and what it normalizes on the way through.

// ulidTenant matches tenants.id: a CHAR(26) ULID. The gate refuses anything
// else so a bad id surfaces as a 400 instead of a database rejection.
const ulidTenant = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func TestNormalizeGetInputBounds(t *testing.T) {
	tests := []struct {
		name string
		in   GetInput

		expectField string

		wantTenant string
		wantID     int64
	}{
		{
			name:        "id zero is refused",
			in:          GetInput{TenantID: ulidTenant, ID: 0},
			expectField: "id",
		},
		{
			name:        "a negative id is refused",
			in:          GetInput{TenantID: ulidTenant, ID: -3},
			expectField: "id",
		},
		{
			name:       "id one is the accepted floor",
			in:         GetInput{TenantID: ulidTenant, ID: 1},
			wantTenant: ulidTenant,
			wantID:     1,
		},
		{
			name:        "a blank tenant has no default to fall back to",
			in:          GetInput{ID: 7},
			expectField: "tenant_id",
		},
		{
			name:        "a short tenant id is refused",
			in:          GetInput{TenantID: "finance", ID: 7},
			expectField: "tenant_id",
		},
		{
			name:       "the tenant is trimmed",
			in:         GetInput{TenantID: "  " + ulidTenant + "  ", ID: 7},
			wantTenant: ulidTenant,
			wantID:     7,
		},
		{
			name:       "a tenant of exactly 26 characters is the accepted shape",
			in:         GetInput{TenantID: ulidTenant, ID: 7},
			wantTenant: ulidTenant,
			wantID:     7,
		},
		{
			name:        "a tenant wider than the column is refused",
			in:          GetInput{TenantID: ulidTenant + "x", ID: 7},
			expectField: "tenant_id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := NormalizeGetInput(tc.in)

			if tc.expectField != "" {
				var verr *validation.Error
				if !errors.As(err, &verr) {
					t.Fatalf("got %v (%T), want *validation.Error", err, err)
				}
				if verr.Field != tc.expectField {
					t.Fatalf("refusal names field %q, want %q", verr.Field, tc.expectField)
				}
				if strings.TrimSpace(verr.Message) == "" {
					t.Fatal("refusal carries an empty message")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if out.TenantID != tc.wantTenant || out.ID != tc.wantID {
				t.Fatalf("normalized to tenant=%q id=%d, want tenant=%q id=%d",
					out.TenantID, out.ID, tc.wantTenant, tc.wantID)
			}
		})
	}
}

func TestNormalizeGetInputRefusesScopedCaller(t *testing.T) {
	// A run has no resource group, so a scoped caller is refused rather than
	// served the whole tenant's ledger under a narrower promise.
	_, err := NormalizeGetInput(GetInput{TenantID: ulidTenant, ID: 7, ResourceGroupID: "rg-7"})
	var serr *resource.ScopeError
	if !errors.As(err, &serr) {
		t.Fatalf("got %v (%T), want *resource.ScopeError", err, err)
	}
	if serr.Resource != "run" || serr.Scope != "rg-7" {
		t.Fatalf("scope refusal = %+v, want resource %q scope %q", serr, "run", "rg-7")
	}
}

func TestNormalizeListInputBounds(t *testing.T) {
	tests := []struct {
		name string
		in   ListInput

		expectField string

		wantTenant string
		wantPhase  string
		wantLimit  int
		wantOffset int
	}{
		{
			name:        "a blank tenant is refused",
			in:          ListInput{},
			expectField: "tenant_id",
		},
		{
			name:       "the tenant is trimmed",
			in:         ListInput{TenantID: "  " + ulidTenant + "  "},
			wantTenant: ulidTenant,
			wantLimit:  DefaultListLimit,
		},
		{
			name:       "an empty phase lists every phase",
			in:         ListInput{TenantID: ulidTenant},
			wantTenant: ulidTenant,
			wantPhase:  "",
			wantLimit:  DefaultListLimit,
		},
		{
			name:       "a known phase is accepted and trimmed",
			in:         ListInput{TenantID: ulidTenant, Phase: "  Running  "},
			wantTenant: ulidTenant,
			wantPhase:  string(jobrun.Running),
			wantLimit:  DefaultListLimit,
		},
		{
			name:        "an unknown phase is refused, not an empty result",
			in:          ListInput{TenantID: ulidTenant, Phase: "runing"},
			expectField: "phase",
		},
		{
			name:       "another listable phase passes the gate",
			in:         ListInput{TenantID: ulidTenant, Phase: string(jobrun.Succeeded)},
			wantTenant: ulidTenant,
			wantPhase:  string(jobrun.Succeeded),
			wantLimit:  DefaultListLimit,
		},
		{
			name:       "limit zero gets the default page size",
			in:         ListInput{TenantID: ulidTenant, Limit: 0},
			wantTenant: ulidTenant,
			wantLimit:  DefaultListLimit,
		},
		{
			name:       "the max page size is the accepted bound",
			in:         ListInput{TenantID: ulidTenant, Limit: MaxListLimit},
			wantTenant: ulidTenant,
			wantLimit:  MaxListLimit,
		},
		{
			name:        "a page above the cap is refused",
			in:          ListInput{TenantID: ulidTenant, Limit: MaxListLimit + 1},
			expectField: "limit",
		},
		{
			name:        "a negative limit is refused",
			in:          ListInput{TenantID: ulidTenant, Limit: -1},
			expectField: "limit",
		},
		{
			name:        "a negative offset is refused",
			in:          ListInput{TenantID: ulidTenant, Offset: -1},
			expectField: "offset",
		},
		{
			name:       "offset zero is accepted",
			in:         ListInput{TenantID: ulidTenant, Offset: 0},
			wantTenant: ulidTenant,
			wantLimit:  DefaultListLimit,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := NormalizeListInput(tc.in)

			if tc.expectField != "" {
				var verr *validation.Error
				if !errors.As(err, &verr) {
					t.Fatalf("got %v (%T), want *validation.Error", err, err)
				}
				if verr.Field != tc.expectField {
					t.Fatalf("refusal names field %q, want %q", verr.Field, tc.expectField)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if out.TenantID != tc.wantTenant || out.Phase != tc.wantPhase ||
				out.Limit != tc.wantLimit || out.Offset != tc.wantOffset {
				t.Fatalf("normalized to tenant=%q phase=%q limit=%d offset=%d, want tenant=%q phase=%q limit=%d offset=%d",
					out.TenantID, out.Phase, out.Limit, out.Offset, tc.wantTenant, tc.wantPhase, tc.wantLimit, tc.wantOffset)
			}
		})
	}
}

func TestNormalizeListInputRefusesScopedCaller(t *testing.T) {
	_, err := NormalizeListInput(ListInput{TenantID: ulidTenant, ResourceGroupID: "rg-7"})
	var serr *resource.ScopeError
	if !errors.As(err, &serr) {
		t.Fatalf("got %v (%T), want *resource.ScopeError", err, err)
	}
	if serr.Resource != "run" || serr.Scope != "rg-7" {
		t.Fatalf("scope refusal = %+v, want resource %q scope %q", serr, "run", "rg-7")
	}
}

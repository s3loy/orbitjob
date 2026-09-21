package query

import (
	"errors"
	"testing"

	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// NormalizeGetInput and NormalizeListInput are the read gates for job
// definitions. Unlike runs, a definition read refuses a scoped caller because a
// definition has no group column to filter on -- serving the whole tenant under
// a narrower promise would overstate the key's reach.

// ulidTenant matches tenants.id: a CHAR(26) ULID. The gate refuses anything
// else so a bad id surfaces as a 400 instead of a database rejection, and
// there is no default tenant to fall back to.
const ulidTenant = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func TestNormalizeJobGetInputBounds(t *testing.T) {
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
			name:       "id one is the accepted floor",
			in:         GetInput{TenantID: ulidTenant, ID: 1},
			wantTenant: ulidTenant,
			wantID:     1,
		},
		{
			name:        "a blank tenant is refused",
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

func TestNormalizeJobGetInputRefusesScopedCaller(t *testing.T) {
	_, err := NormalizeGetInput(GetInput{TenantID: ulidTenant, ID: 7, ResourceGroupID: "rg-7"})
	var serr *resource.ScopeError
	if !errors.As(err, &serr) {
		t.Fatalf("got %v (%T), want *resource.ScopeError", err, err)
	}
	// The refusal names the definition as the resource, because the same gate
	// shape is reused across read models and the verb must say what was denied.
	if serr.Resource != "job definition" || serr.Scope != "rg-7" {
		t.Fatalf("scope refusal = %+v, want resource %q scope %q", serr, "job definition", "rg-7")
	}
}

func TestNormalizeJobListInputBounds(t *testing.T) {
	tests := []struct {
		name string
		in   ListInput

		expectField string

		wantTenant string
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
			if out.TenantID != tc.wantTenant || out.Limit != tc.wantLimit || out.Offset != tc.wantOffset {
				t.Fatalf("normalized to tenant=%q limit=%d offset=%d, want tenant=%q limit=%d offset=%d",
					out.TenantID, out.Limit, out.Offset, tc.wantTenant, tc.wantLimit, tc.wantOffset)
			}
		})
	}
}

func TestNormalizeJobListInputRefusesScopedCaller(t *testing.T) {
	_, err := NormalizeListInput(ListInput{TenantID: ulidTenant, ResourceGroupID: "rg-7"})
	var serr *resource.ScopeError
	if !errors.As(err, &serr) {
		t.Fatalf("got %v (%T), want *resource.ScopeError", err, err)
	}
	if serr.Resource != "job definition" || serr.Scope != "rg-7" {
		t.Fatalf("scope refusal = %+v, want resource %q scope %q", serr, "job definition", "rg-7")
	}
}

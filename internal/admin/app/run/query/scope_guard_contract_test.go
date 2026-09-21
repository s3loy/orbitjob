package query

import (
	"errors"
	"strings"
	"testing"

	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// The two run gates refuse a scoped caller because a run has no group column:
// serving one would hand the caller the whole tenant's ledger under a narrower
// promise. The run detail gate is also the gate in front of the cancel target
// lookup, so these tests guard that path too. The refusal itself is already
// pinned by normalize_test.go; these tests prove the guard is load-bearing --
// that the RequireUnscoped call is the only thing rejecting a scoped request --
// by comparing the real gates against in-test copies with just that call
// removed, the mutant of the red proof. The 403 mapping of the refusal is
// verified by inspection of internal/admin/http/errors.go, not by a test here.

// Fixtures inserted by this file. Tenant ids are tenants.id values, CHAR(26)
// ULIDs; slugs like "default" are never tenant identifiers. t3ScopedGroup is a
// resource group id, a different kind of identifier and never used as a tenant.
const (
	t3Tenant      = "01J9Z7V2M4QK7P3XWQ5R8TNBCE"
	t3ScopedGroup = "01JBB0W9YRXG4SZV2QKM78N3PD"
)

// t3NormalizeRunGetWithoutScopeGuard is NormalizeGetInput with the
// resource.RequireUnscoped call removed and nothing else changed.
func t3NormalizeRunGetWithoutScopeGuard(in GetInput) (GetInput, error) {
	if in.ID < 1 {
		return GetInput{}, validation.New("id", "must be >= 1")
	}

	tenantID := strings.TrimSpace(in.TenantID)
	if len(tenantID) != 26 {
		return GetInput{}, validation.New("tenant_id", "must be a 26-character tenant id")
	}

	return GetInput{TenantID: tenantID, ID: in.ID, ResourceGroupID: in.ResourceGroupID}, nil
}

// t3NormalizeRunListWithoutScopeGuard is NormalizeListInput with the
// resource.RequireUnscoped call removed and nothing else changed.
func t3NormalizeRunListWithoutScopeGuard(in ListInput) (ListInput, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if len(tenantID) != 26 {
		return ListInput{}, validation.New("tenant_id", "must be a 26-character tenant id")
	}

	phase := strings.TrimSpace(in.Phase)
	if phase != "" {
		if _, ok := listablePhases[phase]; !ok {
			return ListInput{}, validation.New("phase", "is not a known run phase")
		}
	}

	limit := in.Limit
	if limit == 0 {
		limit = DefaultListLimit
	}
	if limit < 1 || limit > MaxListLimit {
		return ListInput{}, validation.Errorf("limit", "must be between 1 and %d", MaxListLimit)
	}

	offset := in.Offset
	if offset < 0 {
		return ListInput{}, validation.New("offset", "must be >= 0")
	}

	return ListInput{
		TenantID:        tenantID,
		Phase:           phase,
		ResourceGroupID: in.ResourceGroupID,
		Limit:           limit,
		Offset:          offset,
	}, nil
}

func TestRunGetScopeGuardIsLoadBearing(t *testing.T) {
	scoped := GetInput{TenantID: t3Tenant, ID: 7, ResourceGroupID: t3ScopedGroup}

	_, realErr := NormalizeGetInput(scoped)
	var serr *resource.ScopeError
	if !errors.As(realErr, &serr) {
		t.Fatalf("scoped caller got %v (%T), want *resource.ScopeError", realErr, realErr)
	}
	if serr.Resource != "run" || serr.Scope != t3ScopedGroup {
		t.Fatalf("refusal = %+v, want resource %q scope %q", serr, "run", t3ScopedGroup)
	}

	// The mutant with the guard removed serves the identical request: no other
	// check in the gate rejects it. If this ever stops holding, the assertion
	// above no longer proves the guard is what protects the read -- and with it
	// the cancel target lookup that shares this gate.
	if _, mutantErr := t3NormalizeRunGetWithoutScopeGuard(scoped); mutantErr != nil {
		t.Fatalf("mutant without the guard refused the same request with %v; the refusal does not come from the guard alone", mutantErr)
	}

	// Control: the same caller without the scope is served, so the refusal is
	// the scope and nothing else about the request.
	out, err := NormalizeGetInput(GetInput{TenantID: t3Tenant, ID: 7})
	if err != nil {
		t.Fatalf("unscoped caller refused: %v", err)
	}
	if out.TenantID != t3Tenant || out.ID != 7 {
		t.Fatalf("normalized to tenant=%q id=%d, want tenant=%q id=%d", out.TenantID, out.ID, t3Tenant, 7)
	}
}

func TestRunListScopeGuardIsLoadBearing(t *testing.T) {
	scoped := ListInput{TenantID: t3Tenant, ResourceGroupID: t3ScopedGroup}

	_, realErr := NormalizeListInput(scoped)
	var serr *resource.ScopeError
	if !errors.As(realErr, &serr) {
		t.Fatalf("scoped caller got %v (%T), want *resource.ScopeError", realErr, realErr)
	}
	if serr.Resource != "run" || serr.Scope != t3ScopedGroup {
		t.Fatalf("refusal = %+v, want resource %q scope %q", serr, "run", t3ScopedGroup)
	}

	// The mutant without the guard runs the rest of the pipeline and hands back
	// a fully normalized page: the scoped request was one guard call away from
	// being served.
	mutantOut, mutantErr := t3NormalizeRunListWithoutScopeGuard(scoped)
	if mutantErr != nil {
		t.Fatalf("mutant without the guard refused the same request with %v; the refusal does not come from the guard alone", mutantErr)
	}
	if mutantOut.Limit != DefaultListLimit || mutantOut.Offset != 0 || mutantOut.TenantID != t3Tenant {
		t.Fatalf("mutant normalized to %+v, want a fully served page for tenant %q", mutantOut, t3Tenant)
	}

	// Control: unscoped callers are served and keep the guard out of the way.
	out, err := NormalizeListInput(ListInput{TenantID: t3Tenant})
	if err != nil {
		t.Fatalf("unscoped caller refused: %v", err)
	}
	if out.Limit != DefaultListLimit {
		t.Fatalf("unscoped page limit %d, want the default %d", out.Limit, DefaultListLimit)
	}
}

// t3AssertTenantIdRefusal pins the shape of a tenant_id refusal: the typed
// *validation.Error on field tenant_id, which the admin http layer maps to
// 400 VALIDATION_ERROR -- never a 500, never a silent fallback.
func t3AssertTenantIdRefusal(t *testing.T, err error) {
	t.Helper()
	var verr *validation.Error
	if !errors.As(err, &verr) {
		t.Fatalf("got %v (%T), want *validation.Error", err, err)
	}
	if verr.Field != "tenant_id" {
		t.Fatalf("refusal names field %q, want %q", verr.Field, "tenant_id")
	}
	if strings.TrimSpace(verr.Message) == "" {
		t.Fatal("refusal carries an empty message")
	}
}

func TestRunQueryTenantContract(t *testing.T) {
	// tenant_id on both run gates is tenants.id: exactly 26 characters, no
	// slug, no default. The length bounds around this floor are pinned by
	// normalize_test.go; here the fixtures are this file's own ULIDs and the
	// slug a deployment is most likely to confuse for one.
	t.Run("get", func(t *testing.T) {
		out, err := NormalizeGetInput(GetInput{TenantID: t3Tenant, ID: 7})
		if err != nil {
			t.Fatalf("26-character tenant refused: %v", err)
		}
		if out.TenantID != t3Tenant {
			t.Fatalf("tenant normalized to %q, want %q", out.TenantID, t3Tenant)
		}

		_, err = NormalizeGetInput(GetInput{TenantID: "default", ID: 7})
		t3AssertTenantIdRefusal(t, err)
	})

	t.Run("list", func(t *testing.T) {
		out, err := NormalizeListInput(ListInput{TenantID: t3Tenant})
		if err != nil {
			t.Fatalf("26-character tenant refused: %v", err)
		}
		if out.TenantID != t3Tenant {
			t.Fatalf("tenant normalized to %q, want %q", out.TenantID, t3Tenant)
		}

		_, err = NormalizeListInput(ListInput{TenantID: "default"})
		t3AssertTenantIdRefusal(t, err)
	})
}

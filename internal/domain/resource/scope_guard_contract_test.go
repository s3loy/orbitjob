package resource

import (
	"errors"
	"fmt"
	"testing"
)

// RequireUnscoped is the guard behind all five admin gates that face a scope a
// resource cannot honor: the two run reads, the two job definition reads, and
// the manual trigger. The 403 contract is completed one layer up, in
// internal/admin/http/errors.go, where a *ScopeError maps to CodeForbidden via
// errors.As; that mapping is verified by inspection (an in-package test cannot
// import the http package without an import cycle), so the tests here pin the
// properties the mapping relies on: the concrete *ScopeError type and its
// survival through %w wrapping.
//
// The stub below is the red proof's mutant: it has the guard's signature but
// omits the scope check. Wherever the real guard refuses and the stub accepts,
// the guard -- and nothing else at the call site -- is what stood between a
// scoped caller and a wider view.

// omittedGuardStub is RequireUnscoped with the check removed. It never returns
// an error, which is exactly the failure mode a deleted or short-circuited
// guard produces in production.
func omittedGuardStub(_, _ string) error {
	return nil
}

// t3GuardScope stands in for a resource group id. It is never a tenant id:
// scopes and tenants are different identifiers, and this file keeps them apart.
const t3GuardScope = "01JBB0W9YRXG4SZV2QKM78N3PD"

func TestRequireUnscopedFailsClosed(t *testing.T) {
	// The only unscoped caller is the empty scope. Anything else -- including
	// values that look like defaults or whitespace -- is a carried scope and
	// must be refused verbatim: trimming or defaulting a scope could only ever
	// widen what the caller's grant covers.
	scopes := []string{" ", "\t", "default", t3GuardScope}

	for _, scope := range scopes {
		err := RequireUnscoped(scope, "run")
		if err == nil {
			t.Fatalf("scope %q was let through; only the empty scope is unscoped", scope)
		}
		var serr *ScopeError
		if !errors.As(err, &serr) {
			t.Fatalf("scope %q refused with %T, want *ScopeError", scope, err)
		}
		if serr.Scope != scope {
			t.Fatalf("refusal rewrote the scope to %q, want it verbatim %q", serr.Scope, scope)
		}
		if serr.Resource != "run" {
			t.Fatalf("refusal names resource %q, want %q", serr.Resource, "run")
		}
	}
}

func TestRequireUnscopedRefusesWhatTheOmittedGuardAccepts(t *testing.T) {
	// The resource names exactly as the five production gates pass them, three
	// job definition gates and two run gates. For the same scoped request the
	// real guard must refuse and the check-omitting stub must serve: any other
	// outcome means the refusal no longer comes from the guard.
	for _, resourceKind := range []string{"job definition", "run"} {
		real := RequireUnscoped(t3GuardScope, resourceKind)
		if real == nil {
			t.Fatalf("%s gate: the real guard served a scoped caller", resourceKind)
		}
		var serr *ScopeError
		if !errors.As(real, &serr) {
			t.Fatalf("%s gate: refusal is %T, want *ScopeError", resourceKind, real)
		}
		if stub := omittedGuardStub(t3GuardScope, resourceKind); stub != nil {
			t.Fatalf("%s gate: the stub omitting the check refused; it no longer models a deleted guard", resourceKind)
		}
	}
}

func TestScopeErrorSurvivesWrapping(t *testing.T) {
	// The admin http mapping finds the refusal with errors.As on the wrapped
	// chain, so the typed error must survive the %w wrapping use cases put
	// around gate refusals. If it degraded, a scope refusal would surface as
	// 500 instead of 403.
	wrapped := fmt.Errorf("read definition for trigger: %w", RequireUnscoped(t3GuardScope, "job definition"))

	var serr *ScopeError
	if !errors.As(wrapped, &serr) {
		t.Fatalf("wrapped refusal became %T; the 403 mapping would miss it", wrapped)
	}
	if serr.Resource != "job definition" || serr.Scope != t3GuardScope {
		t.Fatalf("wrapped refusal = %+v, want resource %q scope %q", serr, "job definition", t3GuardScope)
	}
}

package resource

import "testing"

// ScopeError and RequireUnscoped guard the boundary between authorization and
// query serving. The message contract matters because it reaches API callers:
// a scope refusal must say what cannot be reached, and must stay distinct from
// not-found, which is the answer that hides whether a row exists.

func TestScopeError_Error(t *testing.T) {
	err := &ScopeError{Resource: "run", Scope: "rg-7"}
	want := "run is not scoped to rg-7, so a caller limited to that scope cannot reach it"
	if got := err.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestRequireUnscoped(t *testing.T) {
	t.Run("an empty scope passes", func(t *testing.T) {
		if err := RequireUnscoped("", "run"); err != nil {
			t.Fatalf("unscoped caller refused: %v", err)
		}
	})

	t.Run("a carried scope is refused with the resource named", func(t *testing.T) {
		err := RequireUnscoped("rg-7", "job definition")
		se, ok := err.(*ScopeError)
		if !ok {
			t.Fatalf("got %T (%v), want *ScopeError", err, err)
		}
		if se.Resource != "job definition" || se.Scope != "rg-7" {
			t.Fatalf("refusal = %+v, want resource %q scope %q", se, "job definition", "rg-7")
		}
	})

	t.Run("every resource kind must be named, not generic", func(t *testing.T) {
		// The same scope string refused for two different resource kinds must
		// name the kind it was refused for, so the caller can tell which read
		// a scoped key cannot make.
		run := RequireUnscoped("rg-7", "run").Error()
		job := RequireUnscoped("rg-7", "job definition").Error()
		if run == job {
			t.Fatal("scope refusals for different resource kinds are indistinguishable")
		}
	})
}

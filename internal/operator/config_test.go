package operator

import (
	"context"
	"testing"
)

func TestParseNamespaceTenants(t *testing.T) {
	got, err := ParseNamespaceTenants("finance=tenant-a,platform=tenant-b")
	if err != nil {
		t.Fatal(err)
	}
	if got["finance"] != "tenant-a" || got["platform"] != "tenant-b" {
		t.Fatalf("mapping = %v", got)
	}

	trimmed, err := ParseNamespaceTenants(" finance = tenant-a ,, platform=tenant-b ")
	if err != nil {
		t.Fatal(err)
	}
	if trimmed["finance"] != "tenant-a" || trimmed["platform"] != "tenant-b" {
		t.Fatalf("mapping = %v", trimmed)
	}
}

func TestParseNamespaceTenantsRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"no separator", "finance"},
		{"missing namespace", "=tenant-a"},
		{"missing tenant", "finance="},
		{"only separators", ",,"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseNamespaceTenants(tt.raw); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestEnvOr(t *testing.T) {
	const key = "ORBITJOB_OPERATOR_TEST_VALUE"
	t.Setenv(key, "configured")
	if got := EnvOr(key, "fallback"); got != "configured" {
		t.Fatalf("EnvOr = %q", got)
	}
	t.Setenv(key, "")
	if got := EnvOr(key, "fallback"); got != "fallback" {
		t.Fatalf("EnvOr with empty value = %q", got)
	}
	if got := EnvOr("ORBITJOB_UNSET_VALUE", "fallback"); got != "fallback" {
		t.Fatalf("EnvOr for unset key = %q", got)
	}
}

func TestNewControllerWiresHandler(t *testing.T) {
	called := false
	controller := NewController(nil, nil, Config{Workers: 3}, func(_ context.Context, _ string) error {
		called = true
		return nil
	})
	if controller.Reconcile == nil {
		t.Fatal("handler was not wired")
	}
	if err := controller.Reconcile(context.Background(), "jobruns:finance/x"); err != nil || !called {
		t.Fatalf("handler not invoked: %v", err)
	}
	if controller.Config.Workers != 3 {
		t.Fatalf("config not copied: %+v", controller.Config)
	}
}

func TestRuntimeHandlersExposeEveryResource(t *testing.T) {
	handlers := Runtime{}.Handlers()
	if handlers.ReconcileScheduledJob == nil || handlers.ReconcileJobRun == nil || handlers.ReconcileJob == nil {
		t.Fatal("every watched resource must have a handler")
	}
}

func TestRuntimeDefaults(t *testing.T) {
	runtime := Runtime{}
	if runtime.now().IsZero() {
		t.Fatal("now must default to the wall clock")
	}
}

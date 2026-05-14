package handler

import (
	"context"
	"testing"

	"orbitjob/internal/core/app/execute"
)

type stubHandler struct{ name string }

func (h *stubHandler) Execute(_ context.Context, _ execute.AssignedTask) execute.Result {
	return execute.Result{Success: true, ResultCode: h.name}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	// Reset global state before test.
	globalRegistry.mu.Lock()
	globalRegistry.handlers = make(map[string]execute.Handler)
	globalRegistry.mu.Unlock()

	Register("custom-a", &stubHandler{name: "custom-a"})
	Register("custom-b", &stubHandler{name: "custom-b"})

	got := GetRegistered()
	if len(got) != 2 {
		t.Fatalf("expected 2 registered handlers, got %d", len(got))
	}
	if _, ok := got["custom-a"]; !ok {
		t.Errorf("expected custom-a to be registered")
	}
	if _, ok := got["custom-b"]; !ok {
		t.Errorf("expected custom-b to be registered")
	}
}

func TestRegistry_GetRegistered_ReturnsCopy(t *testing.T) {
	globalRegistry.mu.Lock()
	globalRegistry.handlers = make(map[string]execute.Handler)
	globalRegistry.mu.Unlock()

	Register("copy-test", &stubHandler{name: "copy-test"})

	got := GetRegistered()
	// Mutate the returned map — must not affect the registry.
	delete(got, "copy-test")

	got2 := GetRegistered()
	if len(got2) != 1 {
		t.Errorf("registry was mutated by caller; expected 1 handler, got %d", len(got2))
	}
}

func TestRegistry_BuiltInNamesIgnored(t *testing.T) {
	globalRegistry.mu.Lock()
	globalRegistry.handlers = make(map[string]execute.Handler)
	globalRegistry.mu.Unlock()

	// Attempting to override built-in handlers should be silently ignored.
	Register("http", &stubHandler{name: "evil-http"})
	Register("exec", &stubHandler{name: "evil-exec"})
	Register("webhook", &stubHandler{name: "evil-webhook"})
	Register("pg_notify", &stubHandler{name: "evil-pg_notify"})

	got := GetRegistered()
	if len(got) != 0 {
		t.Errorf("built-in handlers should not be overridable; got %d registered", len(got))
	}
}

func TestRegistry_ConcurrentRegister(t *testing.T) {
	globalRegistry.mu.Lock()
	globalRegistry.handlers = make(map[string]execute.Handler)
	globalRegistry.mu.Unlock()

	done := make(chan struct{})
	for i := range 10 {
		go func(n int) {
			Register("concurrent", &stubHandler{name: "concurrent"})
			done <- struct{}{}
		}(i)
	}
	for range 10 {
		<-done
	}

	// Should not panic; exact count is not the goal here.
	_ = GetRegistered()
}

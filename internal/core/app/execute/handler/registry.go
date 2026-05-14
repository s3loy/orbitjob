package handler

import (
	"maps"
	"sync"

	"orbitjob/internal/core/app/execute"
)

var builtInHandlers = map[string]bool{
	"exec":      true,
	"http":      true,
	"webhook":   true,
	"pg_notify": true,
}

var globalRegistry = &registry{
	handlers: make(map[string]execute.Handler),
}

type registry struct {
	mu       sync.RWMutex
	handlers map[string]execute.Handler
}

// Register adds a custom handler to the global registry.
// Built-in handler names (http, exec, webhook, pg_notify) are silently ignored
// to prevent accidental override of core functionality.
func Register(name string, h execute.Handler) {
	if builtInHandlers[name] {
		return
	}
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.handlers[name] = h
}

// GetRegistered returns a shallow copy of all registered custom handlers.
// Built-in handlers are not included.
func GetRegistered() map[string]execute.Handler {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	out := make(map[string]execute.Handler, len(globalRegistry.handlers))
	maps.Copy(out, globalRegistry.handlers)
	return out
}

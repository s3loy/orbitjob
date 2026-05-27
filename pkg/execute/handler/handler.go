// Package handler provides built-in task handlers and a global registry
// for custom handler registration.
package handler

import internal "orbitjob/internal/core/app/execute/handler"

// Exec is the built-in command executor handler.
type Exec = internal.Exec

// HTTP is the built-in HTTP request handler.
type HTTP = internal.HTTP

// Webhook is the built-in webhook handler.
type Webhook = internal.Webhook

// PGNotify is the built-in PostgreSQL NOTIFY handler.
type PGNotify = internal.PGNotify

var (
	// Register adds a custom handler to the global registry.
	// Built-in handler names are silently ignored.
	Register = internal.Register
	// GetRegistered returns a shallow copy of all registered custom handlers.
	GetRegistered = internal.GetRegistered
)

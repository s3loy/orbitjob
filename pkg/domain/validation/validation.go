// Package validation provides domain validation error types.
package validation

import internal "orbitjob/internal/domain/validation"

// Error represents a domain validation failure.
type Error = internal.Error

var (
	// Is reports whether err is a validation error.
	Is = internal.Is
	// As extracts a validation error via errors.As.
	As = internal.As
	// New creates a validation error.
	New = internal.New
	// Errorf creates a formatted validation error.
	Errorf = internal.Errorf
)

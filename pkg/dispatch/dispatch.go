// Package dispatch provides the dispatcher use case.
package dispatch

import internal "orbitjob/internal/core/app/dispatch"

// TickUseCase executes one bounded dispatcher batch.
type TickUseCase = internal.TickUseCase

// NewTickUseCase creates a new dispatcher use case.
var NewTickUseCase = internal.NewTickUseCase

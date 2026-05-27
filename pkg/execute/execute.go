// Package execute provides the executor use case and handler interface.
package execute

import internal "orbitjob/internal/core/app/execute"

type (
	TickUseCase      = internal.TickUseCase
	AssignedTask     = internal.AssignedTask
	Result           = internal.Result
	Handler          = internal.Handler
	WorkerPool       = internal.WorkerPool
	DynamicLease     = internal.DynamicLease
	AdaptiveCapacity = internal.AdaptiveCapacity
)

var (
	NewTickUseCase      = internal.NewTickUseCase
	NewWorkerPool       = internal.NewWorkerPool
	NewDynamicLease     = internal.NewDynamicLease
	NewAdaptiveCapacity = internal.NewAdaptiveCapacity
)

// Package schedule provides the scheduler use case.
package schedule

import internal "orbitjob/internal/core/app/schedule"

type (
	TickUseCase      = internal.TickUseCase
	BatchCounts      = internal.BatchCounts
	DueCronJob       = internal.DueCronJob
	ScheduleDecision = internal.ScheduleDecision
	BreakerConfig    = internal.BreakerConfig
	BreakerSignals   = internal.BreakerSignals
	Breaker          = internal.Breaker
	Phase            = internal.Phase
	ControllerState  = internal.ControllerState
)

const (
	PhaseDiscovery = internal.PhaseDiscovery
	PhaseSteady    = internal.PhaseSteady
	PhaseProtect   = internal.PhaseProtect
	PhaseHalfOpen  = internal.PhaseHalfOpen
)

var (
	NewTickUseCase = internal.NewTickUseCase
	DecideSchedule = internal.DecideSchedule
	NewBreaker     = internal.NewBreaker
	UpdateSteady   = internal.UpdateSteady
)

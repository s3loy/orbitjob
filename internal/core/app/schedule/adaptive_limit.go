package schedule

import (
	"math"
	"time"

	domain "orbitjob/internal/core/domain"
)

// Phase represents the current operational phase of the scheduler.
type Phase int

const (
	PhaseDiscovery Phase = iota
	PhaseSteady
	PhaseProtect
	PhaseHalfOpen
)

func (p Phase) String() string {
	switch p {
	case PhaseDiscovery:
		return "discovery"
	case PhaseSteady:
		return "steady"
	case PhaseProtect:
		return "protect"
	case PhaseHalfOpen:
		return "halfopen"
	default:
		return "unknown"
	}
}

// ControllerState holds mutable state shared across all phases.
type ControllerState struct {
	Limit        int
	LongtermRtt  time.Duration
	MaxBatchSize int
}

const (
	defaultLongWindow = 600
	defaultSmoothing  = 0.2
	defaultAlphaRatio = 0.05
	defaultBetaRatio  = 0.2
)

// UpdateSteady computes the next batch limit using Vegas x Gradient2,
// then applies backpressure based on downstream queue depth.
// Returns the new limit and the Vegas dbPressure estimate.
// The caller must check errClass for FatalWorthy before calling.
func UpdateSteady(state *ControllerState, probeRTT time.Duration, errClass domain.ErrorClass, maxBatchSize int, queueDepth int64) (int, float64) {
	if state.LongtermRtt == 0 {
		state.LongtermRtt = probeRTT
	}

	decay := 1.0 / defaultLongWindow
	state.LongtermRtt = time.Duration(float64(state.LongtermRtt)*(1-decay) + float64(probeRTT)*decay)

	const minProbeRTT = time.Millisecond
	if state.LongtermRtt == 0 || probeRTT < minProbeRTT {
		return state.Limit, 0
	}

	dbPressure := float64(state.Limit) * (1 - float64(state.LongtermRtt)/float64(probeRTT))
	alpha := math.Min(math.Max(3, defaultAlphaRatio*float64(state.Limit)), 25)
	beta := math.Max(6, defaultBetaRatio*float64(state.Limit))

	var newLimit int

	if errClass == domain.BackoffWorthy || dbPressure > beta {
		gradient := math.Max(0.5, float64(state.LongtermRtt)/float64(probeRTT))
		rawLimit := int(gradient * float64(state.Limit))
		newLimit = int(defaultSmoothing*float64(rawLimit) + (1-defaultSmoothing)*float64(state.Limit))
	} else if dbPressure < alpha {
		newLimit = state.Limit + int(alpha)
	} else {
		newLimit = state.Limit
	}

	// Backpressure: throttle when downstream queue is deep.
	// When queueDepth == maxBatchSize, limit is halved.
	// When queueDepth >= maxBatchSize*2, limit approaches 1.
	if queueDepth > 0 {
		bpFactor := float64(maxBatchSize) / float64(maxBatchSize+int(queueDepth))
		newLimit = int(float64(newLimit) * bpFactor)
	}

	newLimit = max(newLimit, 1)
	newLimit = min(newLimit, maxBatchSize)
	state.Limit = newLimit
	return newLimit, dbPressure
}

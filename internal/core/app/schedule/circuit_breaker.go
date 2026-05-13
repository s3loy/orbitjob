package schedule

import (
	"math"
	"time"
)

// BreakerConfig holds configuration for the dual-path circuit breaker.
type BreakerConfig struct {
	Path1Consecutive int
	Path2Consecutive int
	Path2Ratio       float64
	Path2MinAbs      time.Duration
	BackoffBase      time.Duration
	BackoffMax       time.Duration
}

// BreakerSignals are the inputs to the breaker on each tick.
type BreakerSignals struct {
	ErrorRate    float64
	ProbeRTT     time.Duration
	LongtermRtt  time.Duration
	FatalTrigger bool
}

// Breaker implements a dual-path circuit breaker for fault isolation.
type Breaker struct {
	cfg          BreakerConfig
	currentPhase Phase

	path1Count int
	path2Count int

	retries int
	openAt  time.Time
}

// NewBreaker creates a new circuit breaker in the Steady phase.
func NewBreaker(cfg BreakerConfig) *Breaker {
	return &Breaker{cfg: cfg, currentPhase: PhaseSteady}
}

// Update evaluates breaker signals and returns the current phase.
func (b *Breaker) Update(sigs BreakerSignals) Phase {
	if sigs.FatalTrigger {
		return b.transitionToOpen()
	}

	if b.currentPhase == PhaseProtect {
		backoff := time.Duration(math.Min(
			float64(b.cfg.BackoffBase)*math.Pow(2, float64(b.retries)),
			float64(b.cfg.BackoffMax),
		))
		if time.Since(b.openAt) >= backoff {
			b.currentPhase = PhaseHalfOpen
			return PhaseHalfOpen
		}
		return PhaseProtect
	}

	if b.currentPhase == PhaseHalfOpen {
		return PhaseHalfOpen
	}

	// Path 1: error rate > 50%
	if sigs.ErrorRate > 0.5 {
		b.path1Count++
		if b.path1Count >= b.cfg.Path1Consecutive {
			return b.transitionToOpen()
		}
	} else {
		b.path1Count = 0
	}

	// Path 2: probeRTT > max(longtermRtt x ratio, minAbs)
	threshold := time.Duration(math.Max(float64(sigs.LongtermRtt)*b.cfg.Path2Ratio, float64(b.cfg.Path2MinAbs)))
	if sigs.ProbeRTT > threshold {
		b.path2Count++
		if b.path2Count >= b.cfg.Path2Consecutive {
			return b.transitionToOpen()
		}
	} else {
		b.path2Count = 0
	}

	return PhaseSteady
}

func (b *Breaker) transitionToOpen() Phase {
	b.currentPhase = PhaseProtect
	b.openAt = time.Now()
	b.retries++
	b.path1Count = 0
	b.path2Count = 0
	return PhaseProtect
}

// HalfOpenSuccess transitions from HalfOpen to Discovery after a successful probe.
func (b *Breaker) HalfOpenSuccess(prevLimit int) (Phase, int) {
	b.retries = 0
	limit := min(5, prevLimit/10)
	if limit < 1 {
		limit = 1
	}
	return PhaseDiscovery, limit
}

// HalfOpenFailure transitions back to Protect after a failed HalfOpen probe.
func (b *Breaker) HalfOpenFailure() {
	b.currentPhase = PhaseProtect
	b.openAt = time.Now()
	b.retries++
}

// State returns the current breaker phase.
func (b *Breaker) State() Phase {
	return b.currentPhase
}

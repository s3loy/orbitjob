package schedule

import (
	"testing"
	"time"
)

func testBreakerConfig() BreakerConfig {
	return BreakerConfig{
		Path1Consecutive: 2,
		Path2Consecutive: 3,
		Path2Ratio:       5,
		Path2MinAbs:      500 * time.Millisecond,
		BackoffBase:      5 * time.Second,
		BackoffMax:       300 * time.Second,
	}
}

func TestBreaker_Path1OpensOnConsecutiveErrors(t *testing.T) {
	b := NewBreaker(testBreakerConfig())

	state := b.Update(BreakerSignals{ErrorRate: 0.6, ProbeRTT: 1 * time.Millisecond, LongtermRtt: 1 * time.Millisecond})
	if state != PhaseSteady {
		t.Fatalf("tick 1: expected Steady, got %v", state)
	}

	state = b.Update(BreakerSignals{ErrorRate: 0.6, ProbeRTT: 1 * time.Millisecond, LongtermRtt: 1 * time.Millisecond})
	if state != PhaseProtect {
		t.Fatalf("tick 2: expected Protect (path 1), got %v", state)
	}
}

func TestBreaker_Path1ResetsOnLowErrorRate(t *testing.T) {
	b := NewBreaker(testBreakerConfig())

	b.Update(BreakerSignals{ErrorRate: 0.6, ProbeRTT: 1 * time.Millisecond, LongtermRtt: 1 * time.Millisecond})
	state := b.Update(BreakerSignals{ErrorRate: 0.3, ProbeRTT: 1 * time.Millisecond, LongtermRtt: 1 * time.Millisecond})
	if state != PhaseSteady {
		t.Fatalf("expected Steady after path1 reset, got %v", state)
	}
}

func TestBreaker_Path2OpensOnHighRTT(t *testing.T) {
	b := NewBreaker(testBreakerConfig())

	baseRTT := 10 * time.Millisecond
	highRTT := 600 * time.Millisecond

	for i := 0; i < 3; i++ {
		state := b.Update(BreakerSignals{ErrorRate: 0, ProbeRTT: highRTT, LongtermRtt: baseRTT})
		if i < 2 {
			if state != PhaseSteady {
				t.Fatalf("tick %d: expected Steady, got %v", i+1, state)
			}
		} else {
			if state != PhaseProtect {
				t.Fatalf("tick %d: expected Protect (path 2), got %v", i+1, state)
			}
		}
	}
}

func TestBreaker_FatalImmediate(t *testing.T) {
	b := NewBreaker(testBreakerConfig())

	state := b.Update(BreakerSignals{FatalTrigger: true})
	if state != PhaseProtect {
		t.Fatalf("expected immediate Protect on Fatal, got %v", state)
	}
}

func TestBreaker_HalfOpenSuccess(t *testing.T) {
	b := NewBreaker(testBreakerConfig())

	// Force Open
	b.Update(BreakerSignals{FatalTrigger: true})

	// Manually advance to HalfOpen
	b.currentPhase = PhaseHalfOpen

	if b.State() != PhaseHalfOpen {
		t.Fatalf("expected HalfOpen, got %v", b.State())
	}

	nextPhase, newLimit := b.HalfOpenSuccess(100)
	if nextPhase != PhaseDiscovery {
		t.Fatalf("expected Discovery after HalfOpen success, got %v", nextPhase)
	}
	expectedLimit := min(5, 100/10)
	if newLimit != expectedLimit {
		t.Fatalf("expected HalfOpen Limit=%d, got %d", expectedLimit, newLimit)
	}
}

func TestBreaker_HalfOpenFailure(t *testing.T) {
	b := NewBreaker(testBreakerConfig())

	// Force Open
	b.Update(BreakerSignals{FatalTrigger: true})
	b.currentPhase = PhaseHalfOpen

	b.HalfOpenFailure()
	if b.State() != PhaseProtect {
		t.Fatalf("expected Protect after HalfOpen failure, got %v", b.State())
	}
}

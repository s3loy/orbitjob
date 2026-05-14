package execute

import (
	"log/slog"
	"math"
	"time"

	"orbitjob/internal/platform/metrics"
)

// AdaptiveCapacity controls how many tasks a worker claims per tick.
// It uses queue depth, active worker count, and DB RTT to converge on a
// stable capacity that maximizes throughput without overloading the database.
type AdaptiveCapacity struct {
	maxCapacity   int
	rttThreshold  float64 // multiplier over baseline RTT that triggers reduction
	current       int
	emaSmoothing  float64 // EMA coefficient for capacity smoothing
	longtermRtt   time.Duration
}

// NewAdaptiveCapacity creates a controller with the given upper bound.
// maxCapacity must be >= 1.
func NewAdaptiveCapacity(maxCapacity int, rttThreshold float64) *AdaptiveCapacity {
	if maxCapacity < 1 {
		maxCapacity = 1
	}
	if rttThreshold <= 0 {
		rttThreshold = 3
	}
	return &AdaptiveCapacity{
		maxCapacity:  maxCapacity,
		rttThreshold: rttThreshold,
		current:      1, // start conservative
		emaSmoothing: 0.3,
		longtermRtt:  0,
	}
}

// Current returns the capacity to use for the next claim.
func (ac *AdaptiveCapacity) Current() int { return ac.current }

// Max returns the hard upper bound.
func (ac *AdaptiveCapacity) Max() int { return ac.maxCapacity }

// Update computes the next capacity based on queue depth, worker count,
// and a DB probe RTT.  It updates internal state and returns the new capacity.
func (ac *AdaptiveCapacity) Update(
	probeRtt time.Duration,
	queueDepth, activeWorkers int64,
	workerID, tenantID string,
) int {
	// Update long-term RTT baseline with a light EMA.
	if ac.longtermRtt == 0 {
		ac.longtermRtt = probeRtt
	} else {
		decay := 0.1
		ac.longtermRtt = time.Duration(float64(ac.longtermRtt)*(1-decay) + float64(probeRtt)*decay)
	}

	metrics.WorkerQueueDepth.WithLabelValues(workerID, tenantID).Set(float64(queueDepth))

	if activeWorkers < 1 {
		activeWorkers = 1
	}

	// Target capacity: share of queue depth per worker, clamped to [1, max].
	target := int(math.Ceil(float64(queueDepth) / float64(activeWorkers)))
	target = max(target, 1)
	target = min(target, ac.maxCapacity)

	// RTT pressure check: if DB is slow, halve the target.
	if ac.longtermRtt > 0 {
		threshold := time.Duration(float64(ac.longtermRtt) * ac.rttThreshold)
		if probeRtt > threshold {
			target = max(target/2, 1)
			slog.Debug("adaptive capacity: RTT pressure detected, reducing target",
				"probe_rtt_ms", probeRtt.Milliseconds(),
				"threshold_ms", threshold.Milliseconds(),
				"reduced_target", target)
		}
	}

	// EMA smoothing to avoid oscillation.
	newCapacity := int(ac.emaSmoothing*float64(target) + (1-ac.emaSmoothing)*float64(ac.current))
	newCapacity = max(newCapacity, 1)
	newCapacity = min(newCapacity, ac.maxCapacity)

	if newCapacity != ac.current {
		slog.Info("adaptive capacity updated",
			"from", ac.current, "to", newCapacity,
			"queue_depth", queueDepth,
			"active_workers", activeWorkers,
			"probe_rtt_ms", probeRtt.Milliseconds())
	}

	ac.current = newCapacity
	metrics.WorkerCapacity.WithLabelValues(workerID, tenantID).Set(float64(ac.current))
	return ac.current
}

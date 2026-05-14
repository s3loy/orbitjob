package execute

import (
	"log/slog"
	"time"

	"orbitjob/internal/platform/metrics"
)

// DynamicLease adapts lease duration based on observed task execution times.
// Short-running tasks get short leases (less waste), long-running tasks get
// long leases (less chance of being stolen by orphan recovery).
type DynamicLease struct {
	minDuration time.Duration
	maxDuration time.Duration
	emaDecay    float64 // EMA decay coefficient
	emaDuration time.Duration
}

// NewDynamicLease creates a controller with the given bounds.
// minDuration and maxDuration must be > 0; maxDuration must be >= minDuration.
func NewDynamicLease(minDuration, maxDuration time.Duration, emaDecay float64) *DynamicLease {
	if minDuration <= 0 {
		minDuration = 10 * time.Second
	}
	if maxDuration <= 0 || maxDuration < minDuration {
		maxDuration = 5 * time.Minute
	}
	if emaDecay <= 0 || emaDecay > 1 {
		emaDecay = 0.1
	}
	return &DynamicLease{
		minDuration: minDuration,
		maxDuration: maxDuration,
		emaDecay:    emaDecay,
		emaDuration: 0,
	}
}

// Current returns the lease duration to use for the next claim.
// Before any task has completed it returns the minimum duration.
func (dl *DynamicLease) Current() time.Duration {
	if dl.emaDuration == 0 {
		return dl.minDuration
	}
	// Lease = 3x EMA execution time, clamped to bounds.
	lease := time.Duration(float64(dl.emaDuration) * 3)
	lease = max(lease, dl.minDuration)
	lease = min(lease, dl.maxDuration)
	return lease
}

// RecordExecution updates the EMA with a newly observed execution duration.
func (dl *DynamicLease) RecordExecution(d time.Duration) {
	if d <= 0 {
		return
	}
	if dl.emaDuration == 0 {
		dl.emaDuration = d
	} else {
		dl.emaDuration = time.Duration(
			float64(dl.emaDuration)*(1-dl.emaDecay) + float64(d)*dl.emaDecay,
		)
	}
}

// Update is a convenience method that records execution and returns the new lease.
// Labels are used for Prometheus metrics.
func (dl *DynamicLease) Update(d time.Duration, workerID, tenantID string) time.Duration {
	dl.RecordExecution(d)
	lease := dl.Current()
	metrics.WorkerLeaseDuration.WithLabelValues(workerID, tenantID).Set(lease.Seconds())
	slog.Debug("dynamic lease updated",
		"execution_ms", d.Milliseconds(),
		"ema_ms", dl.emaDuration.Milliseconds(),
		"lease_sec", lease.Seconds())
	return lease
}

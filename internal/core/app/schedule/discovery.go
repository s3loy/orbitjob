package schedule

import (
	"context"
	"time"

	domain "orbitjob/internal/core/domain"
)

// ProbeFn measures round-trip time with a lightweight query.
type ProbeFn func(ctx context.Context) (time.Duration, error)

// DiscoveryBatchFn runs a single batch during discovery.
type DiscoveryBatchFn func(ctx context.Context, limit int) (handled int, class domain.ErrorClass)

// Discovery runs the tight-loop capacity probing phase.
type Discovery struct {
	probe    ProbeFn
	runBatch DiscoveryBatchFn
	state    *ControllerState
}

// NewDiscovery creates a new Discovery phase runner.
func NewDiscovery(probe ProbeFn, runBatch DiscoveryBatchFn, state *ControllerState) *Discovery {
	return &Discovery{probe: probe, runBatch: runBatch, state: state}
}

// Run executes the Discovery phase, probing the system to find the maximum
// sustainable batch size. Returns the next phase (Steady or Protect).
// Uses a single post-batch probe instead of before/after pair to halve RTT cost.
func (d *Discovery) Run(ctx context.Context) Phase {
	d.state.Limit = 1

	for {
		select {
		case <-ctx.Done():
			return PhaseProtect
		default:
		}

		handled, class := d.runBatch(ctx, d.state.Limit)
		if class == domain.FatalWorthy {
			return PhaseProtect
		}

		after, err := d.probe(ctx)
		if err != nil {
			return PhaseProtect
		}

		if class == domain.BackoffWorthy {
			d.state.Limit = max(d.state.Limit/2, 1)
			d.state.LongtermRtt = after
			return PhaseSteady
		}

		// First iteration: establish baseline, no congestion check.
		if d.state.LongtermRtt == 0 {
			d.state.LongtermRtt = after
		} else if after > time.Duration(float64(d.state.LongtermRtt)*1.5) && handled < int(float64(d.state.Limit)*0.9) {
			// RTT degradation vs baseline AND throughput drop.
			d.state.Limit = max(d.state.Limit/2, 1)
			return PhaseSteady
		} else {
			d.state.LongtermRtt = after
		}

		d.state.Limit = min(d.state.Limit*4, d.state.MaxBatchSize)
		if d.state.Limit >= d.state.MaxBatchSize {
			return PhaseSteady
		}
	}
}

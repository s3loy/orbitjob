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
func (d *Discovery) Run(ctx context.Context) Phase {
	d.state.Limit = 1

	for {
		select {
		case <-ctx.Done():
			return PhaseProtect
		default:
		}

		before, err := d.probe(ctx)
		if err != nil {
			return PhaseProtect
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
			d.state.Limit = d.state.Limit / 2
			d.state.LongtermRtt = before
			return PhaseSteady
		}

		// Dual confirmation: RTT degradation AND throughput drop
		if after > time.Duration(float64(before)*1.5) && handled < int(float64(d.state.Limit)*0.9) {
			d.state.Limit = d.state.Limit / 2
			d.state.LongtermRtt = before
			return PhaseSteady
		}

		d.state.Limit = min(d.state.Limit*4, d.state.MaxBatchSize)
		if d.state.Limit >= d.state.MaxBatchSize {
			d.state.LongtermRtt = before
			return PhaseSteady
		}
	}
}

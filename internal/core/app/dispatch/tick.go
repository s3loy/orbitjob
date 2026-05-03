package dispatch

import (
	"context"
	"fmt"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
)

type dispatcher interface {
	DispatchOne(
		ctx context.Context,
		spec domaininstance.ClaimSpec,
		decide func(domaininstance.DispatchInput) domaininstance.DispatchDecision,
	) (_ domaininstance.Snapshot, found bool, _ error)
	RecoverLeaseOrphans(ctx context.Context, now time.Time) (dispatched, running int64, _ error)
	RefreshEffectivePriority(ctx context.Context, now time.Time) (int64, error)
}

// TickUseCase executes one bounded dispatcher batch.
// At the start of each batch it refreshes effective priority and recovers
// orphaned instances (dispatched+running) whose lease has expired.
type TickUseCase struct {
	repo dispatcher
}

func NewTickUseCase(repo dispatcher) *TickUseCase {
	return &TickUseCase{repo: repo}
}

// RunBatch dispatches at most limit eligible instances in one tick.
// It first refreshes effective priority, then recovers orphaned dispatched
// and running instances before attempting normal dispatch.
func (uc *TickUseCase) RunBatch(ctx context.Context, spec domaininstance.ClaimSpec, limit int) (int, error) {
	if limit < 1 {
		limit = 1
	}

	// Refresh effective_priority for all pending/retry_wait instances.
	if _, err := uc.repo.RefreshEffectivePriority(ctx, spec.Now); err != nil {
		return 0, fmt.Errorf("refresh effective priority: %w", err)
	}

	// Recover orphans before dispatching.
	if _, _, err := uc.repo.RecoverLeaseOrphans(ctx, spec.Now); err != nil {
		return 0, fmt.Errorf("recover lease orphans: %w", err)
	}

	handled := 0
	for i := 0; i < limit; i++ {
		_, found, err := uc.repo.DispatchOne(ctx, spec, domaininstance.DecideDispatch)
		if err != nil {
			return handled, err
		}
		if !found {
			break
		}
		handled++
	}

	return handled, nil
}

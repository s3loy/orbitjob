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
	RecoverLeaseOrphans(ctx context.Context, now time.Time) (int64, error)
}

// TickUseCase executes one bounded dispatcher batch.
// At the start of each batch it recovers any orphaned dispatching instances
// whose lease has expired, ensuring partial dispatch attempts are not lost.
type TickUseCase struct {
	repo dispatcher
}

func NewTickUseCase(repo dispatcher) *TickUseCase {
	return &TickUseCase{repo: repo}
}

// RunBatch dispatches at most limit eligible instances in one tick.
// It first recovers any orphaned dispatching instances (lease expired) before
// attempting normal dispatch, preventing jobs from being lost when a
// dispatcher crashes mid-claim.
func (uc *TickUseCase) RunBatch(ctx context.Context, spec domaininstance.ClaimSpec, limit int) (int, error) {
	if limit < 1 {
		limit = 1
	}

	// Recover any orphaned dispatching instances before dispatching.
	if _, err := uc.repo.RecoverLeaseOrphans(ctx, spec.Now); err != nil {
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

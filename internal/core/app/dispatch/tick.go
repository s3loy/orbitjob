package dispatch

import (
	"context"
	"fmt"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
	"orbitjob/internal/platform/metrics"
)

type dispatcher interface {
	DispatchBatch(
		ctx context.Context,
		spec domaininstance.ClaimSpec,
		limit int,
		decide func(domaininstance.DispatchInput) domaininstance.DispatchDecision,
	) (handled int, _ error)
	RecoverLeaseOrphans(ctx context.Context, now time.Time) (dispatched, running int64, _ error)
	RefreshEffectivePriority(ctx context.Context, now time.Time) (int64, error)
	RecoverExpiredWorkers(ctx context.Context, now time.Time) (int64, error)
	ListActiveTenantIDs(ctx context.Context) ([]string, error)
}

// TickUseCase executes one bounded dispatcher batch.
// At the start of each batch it recovers orphaned instances, then refreshes
// effective priority (including recovered pending instances) before dispatching.
type TickUseCase struct {
	repo dispatcher
}

func NewTickUseCase(repo dispatcher) *TickUseCase {
	return &TickUseCase{repo: repo}
}

// RunBatch dispatches at most limit eligible instances in one tick.
func (uc *TickUseCase) RunBatch(ctx context.Context, spec domaininstance.ClaimSpec, limit int) (int, error) {
	if limit < 1 {
		limit = 1
	}

	// Recover orphans first so recovered pending instances get effective_priority
	// recomputed by the subsequent refresh.
	if dispatched, running, err := uc.repo.RecoverLeaseOrphans(ctx, spec.Now); err != nil {
		return 0, fmt.Errorf("recover lease orphans: %w", err)
	} else {
		if dispatched > 0 {
			metrics.DispatcherOrphanRecoveryTotal.WithLabelValues(spec.TenantID, "dispatched").Add(float64(dispatched))
		}
		if running > 0 {
			metrics.DispatcherOrphanRecoveryTotal.WithLabelValues(spec.TenantID, "running").Add(float64(running))
		}
	}

	// Mark workers whose lease expired as offline.
	if n, err := uc.repo.RecoverExpiredWorkers(ctx, spec.Now); err != nil {
		return 0, fmt.Errorf("recover expired workers: %w", err)
	} else if n > 0 {
		metrics.DispatcherOrphanRecoveryTotal.WithLabelValues(spec.TenantID, "worker").Add(float64(n))
	}

	// Refresh effective_priority for all pending/retry_wait instances.
	if _, err := uc.repo.RefreshEffectivePriority(ctx, spec.Now); err != nil {
		return 0, fmt.Errorf("refresh effective priority: %w", err)
	}

	handled, err := uc.repo.DispatchBatch(ctx, spec, limit, domaininstance.DecideDispatch)
	if err != nil {
		return handled, err
	}

	return handled, nil
}

// QuickTick dispatches at most limit eligible instances without running
// housekeeping (orphan recovery, worker recovery, priority refresh).
// Used for event-driven fast path.
func (uc *TickUseCase) QuickTick(ctx context.Context, spec domaininstance.ClaimSpec, limit int) (int, error) {
	if limit < 1 {
		limit = 1
	}
	return uc.repo.DispatchBatch(ctx, spec, limit, domaininstance.DecideDispatch)
}

	func (uc *TickUseCase) ListActiveTenantIDs(ctx context.Context) ([]string, error) {
		return uc.repo.ListActiveTenantIDs(ctx)
	}

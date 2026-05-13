package dispatch

import (
	"context"
	"fmt"
	"log/slog"
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
	TryAdvisoryLock(ctx context.Context) (bool, error)
	ReleaseAdvisoryLock(ctx context.Context) error
	CountQueueDepth(ctx context.Context, tenantID string, now time.Time) (int64, error)
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
// Housekeeping (orphan recovery, worker recovery, priority refresh) is
// performed separately via RunHousekeeping so it runs once per tick
// across all tenants instead of once per tenant.
func (uc *TickUseCase) RunBatch(ctx context.Context, spec domaininstance.ClaimSpec, limit int) (int, error) {
	return uc.dispatchAndRecordDepth(ctx, spec, limit)
}

// QuickTick dispatches at most limit eligible instances without running
// housekeeping. Used for event-driven fast path.
func (uc *TickUseCase) QuickTick(ctx context.Context, spec domaininstance.ClaimSpec, limit int) (int, error) {
	return uc.dispatchAndRecordDepth(ctx, spec, limit)
}

func (uc *TickUseCase) dispatchAndRecordDepth(ctx context.Context, spec domaininstance.ClaimSpec, limit int) (int, error) {
	if limit < 1 {
		limit = 1
	}
	handled, err := uc.repo.DispatchBatch(ctx, spec, limit, domaininstance.DecideDispatch)
	if err != nil {
		return handled, err
	}
	if depth, err := uc.repo.CountQueueDepth(ctx, spec.TenantID, spec.Now); err == nil {
		metrics.DispatcherQueueDepth.WithLabelValues(spec.TenantID).Set(float64(depth))
	}
	return handled, nil
}

// RunHousekeeping performs global cleanup: orphan recovery, worker recovery,
// and priority refresh. It uses an advisory lock to prevent multiple
// dispatcher processes from running housekeeping simultaneously.
func (uc *TickUseCase) RunHousekeeping(ctx context.Context, now time.Time) error {
	acquired, err := uc.repo.TryAdvisoryLock(ctx)
	if err != nil {
		return fmt.Errorf("acquire housekeeping lock: %w", err)
	}
	if !acquired {
		return nil // another dispatcher is doing housekeeping
	}
	defer func() {
		if err := uc.repo.ReleaseAdvisoryLock(context.Background()); err != nil {
			slog.Error("release housekeeping lock failed", "error", err)
		}
	}()

	if dispatched, running, err := uc.repo.RecoverLeaseOrphans(ctx, now); err != nil {
		return fmt.Errorf("recover lease orphans: %w", err)
	} else {
		if dispatched > 0 {
			metrics.DispatcherOrphanRecoveryTotal.WithLabelValues("global", "dispatched").Add(float64(dispatched))
		}
		if running > 0 {
			metrics.DispatcherOrphanRecoveryTotal.WithLabelValues("global", "running").Add(float64(running))
		}
	}

	if n, err := uc.repo.RecoverExpiredWorkers(ctx, now); err != nil {
		return fmt.Errorf("recover expired workers: %w", err)
	} else if n > 0 {
		metrics.DispatcherOrphanRecoveryTotal.WithLabelValues("global", "worker").Add(float64(n))
	}

	if _, err := uc.repo.RefreshEffectivePriority(ctx, now); err != nil {
		return fmt.Errorf("refresh effective priority: %w", err)
	}

	return nil
}

func (uc *TickUseCase) ListActiveTenantIDs(ctx context.Context) ([]string, error) {
	return uc.repo.ListActiveTenantIDs(ctx)
}

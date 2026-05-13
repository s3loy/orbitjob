package dispatch

import (
	"context"
	"fmt"
	"testing"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
)

// ---------------------------------------------------------------------------
// Mock repository
// ---------------------------------------------------------------------------

type benchDispatchRepo struct {
	orphanDispatched int64
	orphanRunning    int64
	orphanErr        error
	priorityAffected int64
	priorityErr      error
	dispatchResults  []bool // true = found and dispatched
	idx              int
}

func (m *benchDispatchRepo) RecoverLeaseOrphans(_ context.Context, _ time.Time) (int64, int64, error) {
	return m.orphanDispatched, m.orphanRunning, m.orphanErr
}

func (m *benchDispatchRepo) RecoverExpiredWorkers(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (m *benchDispatchRepo) ListActiveTenantIDs(_ context.Context) ([]string, error) {
	return []string{"default"}, nil
}

func (m *benchDispatchRepo) RefreshEffectivePriority(_ context.Context, _ time.Time) (int64, error) {
	return m.priorityAffected, m.priorityErr
}

func (m *benchDispatchRepo) DispatchBatch(
	_ context.Context, _ domaininstance.ClaimSpec, limit int,
	_ func(domaininstance.DispatchInput) domaininstance.DispatchDecision,
) (int, error) {
	handled := 0
	for i := 0; i < limit && m.idx < len(m.dispatchResults); i++ {
		if m.dispatchResults[m.idx] {
			handled++
		}
		m.idx++
	}
	return handled, nil
}

func (m *benchDispatchRepo) TryAdvisoryLock(_ context.Context) (bool, error)   { return true, nil }
func (m *benchDispatchRepo) ReleaseAdvisoryLock(_ context.Context) error       { return nil }
func (m *benchDispatchRepo) CountQueueDepth(_ context.Context, _ string, _ time.Time) (int64, error) {
	return 0, nil
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkDispatchTick(b *testing.B) {
	results := make([]bool, 100)
	for i := range results {
		results[i] = true
	}

	claimSpec := domaininstance.ClaimSpec{
		TenantID:       "default",
		Now:            time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
		LeaseExpiresAt: time.Date(2026, 4, 19, 12, 1, 0, 0, time.UTC),
	}

	tests := []struct {
		name            string
		limit           int
		orphanDisp      int64
		orphanRun       int64
		priorityAffected int64
	}{
		{"limit=1_empty_orphans", 1, 0, 0, 0},
		{"limit=10_empty_orphans", 10, 0, 0, 10},
		{"limit=100_empty_orphans", 100, 0, 0, 100},
		{"limit=10_with_5_orphans", 10, 3, 2, 10},
		{"limit=50_with_10_orphans", 50, 7, 3, 50},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				repo := &benchDispatchRepo{
					orphanDispatched: tt.orphanDisp,
					orphanRunning:    tt.orphanRun,
					priorityAffected: tt.priorityAffected,
					dispatchResults:  results,
				}
				uc := NewTickUseCase(repo)
				_, _ = uc.RunBatch(context.Background(), claimSpec, tt.limit)
			}
		})
	}
}

func BenchmarkDispatchTick_OrphanRecoveryError(b *testing.B) {
	claimSpec := domaininstance.ClaimSpec{
		TenantID:       "default",
		Now:            time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
		LeaseExpiresAt: time.Date(2026, 4, 19, 12, 1, 0, 0, time.UTC),
	}

	b.Run("orphan_error", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			repo := &benchDispatchRepo{orphanErr: fmt.Errorf("db error")}
			uc := NewTickUseCase(repo)
			_, _ = uc.RunBatch(context.Background(), claimSpec, 10)
		}
	})

	b.Run("priority_error", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			repo := &benchDispatchRepo{priorityErr: fmt.Errorf("db error")}
			uc := NewTickUseCase(repo)
			_, _ = uc.RunBatch(context.Background(), claimSpec, 10)
		}
	})
}

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
	dispatchErrIdx   map[int]error
	idx              int
}

func (m *benchDispatchRepo) RecoverLeaseOrphans(_ context.Context, _ time.Time) (int64, int64, error) {
	return m.orphanDispatched, m.orphanRunning, m.orphanErr
}

func (m *benchDispatchRepo) RecoverExpiredWorkers(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (m *benchDispatchRepo) RefreshEffectivePriority(_ context.Context, _ time.Time) (int64, error) {
	return m.priorityAffected, m.priorityErr
}

func (m *benchDispatchRepo) DispatchOne(
	_ context.Context, _ domaininstance.ClaimSpec,
	_ func(domaininstance.DispatchInput) domaininstance.DispatchDecision,
) (domaininstance.Snapshot, bool, error) {
	if m.idx >= len(m.dispatchResults) {
		return domaininstance.Snapshot{}, false, nil
	}
	found := m.dispatchResults[m.idx]
	var err error
	if m.dispatchErrIdx != nil {
		err = m.dispatchErrIdx[m.idx]
	}
	m.idx++
	return domaininstance.Snapshot{ID: int64(m.idx), Status: "dispatched"}, found, err
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

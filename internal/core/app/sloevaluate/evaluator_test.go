package sloevaluate

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/slo"
)

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

type stubSLOReader struct {
	listFunc func(ctx context.Context, tenantID string) ([]slo.Snapshot, error)
}

func (s *stubSLOReader) ListActive(ctx context.Context, tenantID string) ([]slo.Snapshot, error) {
	return s.listFunc(ctx, tenantID)
}

type stubSnapshotAggregator struct {
	aggregateFunc func(ctx context.Context, tenantID string, sliID int64, start, end time.Time) (WindowAggregate, error)
}

func (s *stubSnapshotAggregator) AggregateWindow(ctx context.Context, tenantID string, sliID int64, start, end time.Time) (WindowAggregate, error) {
	return s.aggregateFunc(ctx, tenantID, sliID, start, end)
}

type stubBudgetWriter struct {
	budgets []slo.Budget
	err     error
}

func (s *stubBudgetWriter) Upsert(ctx context.Context, tenantID string, budget slo.Budget) (slo.Budget, error) {
	budget.ID = int64(len(s.budgets) + 1)
	s.budgets = append(s.budgets, budget)
	return budget, s.err
}

type stubAlertWriter struct {
	created      []alertCreateCall
	resolved     []alertResolveCall
	activeAlerts []AlertSnapshot
	err          error
}

type alertCreateCall struct {
	tenantID string
	sloID    int64
	budgetID int64
	alertType string
	burnRate float64
}

type alertResolveCall struct {
	tenantID  string
	sloID     int64
	alertType string
}

func (s *stubAlertWriter) Create(ctx context.Context, tenantID string, sloID, budgetID int64, alertType string, burnRate float64) error {
	s.created = append(s.created, alertCreateCall{tenantID: tenantID, sloID: sloID, budgetID: budgetID, alertType: alertType, burnRate: burnRate})
	return s.err
}

func (s *stubAlertWriter) ResolveBySLOAndType(ctx context.Context, tenantID string, sloID int64, alertType string) error {
	s.resolved = append(s.resolved, alertResolveCall{tenantID: tenantID, sloID: sloID, alertType: alertType})
	return s.err
}

func (s *stubAlertWriter) GetActiveBySLO(ctx context.Context, tenantID string, sloID int64) ([]AlertSnapshot, error) {
	return s.activeAlerts, s.err
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestEvaluateAll_NoActiveSLOs(t *testing.T) {
	reader := &stubSLOReader{
		listFunc: func(_ context.Context, _ string) ([]slo.Snapshot, error) {
			return nil, nil
		},
	}
	agg := &stubSnapshotAggregator{}
	bw := &stubBudgetWriter{}
	aw := &stubAlertWriter{}

	uc := NewEvaluateUseCase(reader, agg, bw, aw)
	err := uc.EvaluateAll(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("EvaluateAll() error = %v", err)
	}

	if len(bw.budgets) != 0 {
		t.Fatalf("expected 0 budgets, got %d", len(bw.budgets))
	}
}

func TestEvaluateAll_SLOWithEvents_CalculatesBurnRate(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	reader := &stubSLOReader{
		listFunc: func(_ context.Context, _ string) ([]slo.Snapshot, error) {
			return []slo.Snapshot{
				{
					ID:                1,
					TenantID:          "tenant-a",
					Name:              "availability-slo",
					SLIID:             10,
					Target:            0.99,
					WindowType:        slo.WindowTypeRolling,
					WindowDuration:    30 * 24 * time.Hour,
					AlertFastBurnRate: 14.4,
					AlertSlowBurnRate: 2.0,
					Status:            slo.StatusActive,
				},
			}, nil
		},
	}
	agg := &stubSnapshotAggregator{
		aggregateFunc: func(_ context.Context, _ string, sliID int64, _, _ time.Time) (WindowAggregate, error) {
			if sliID != 10 {
				t.Fatalf("expected sliID=10, got %d", sliID)
			}
			return WindowAggregate{GoodEventsCount: 99, TotalEventsCount: 100}, nil
		},
	}
	bw := &stubBudgetWriter{}
	aw := &stubAlertWriter{activeAlerts: nil}

	uc := NewEvaluateUseCase(reader, agg, bw, aw)
	uc.clock = func() time.Time { return now }

	err := uc.EvaluateAll(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("EvaluateAll() error = %v", err)
	}

	if len(bw.budgets) != 1 {
		t.Fatalf("expected 1 budget, got %d", len(bw.budgets))
	}
	b := bw.budgets[0]

	// target=0.99, total=100, good=99
	// budgetTotal = (1-0.99)*100 = 1
	// sliValue = 99/100 = 0.99
	// budgetConsumed = (1-0.99)*100 = 1
	// budgetRemaining = 1-1 = 0
	// Use tolerance for floating-point comparison.
	const eps = 1e-9
	if b.BudgetTotal < 1.0-eps || b.BudgetTotal > 1.0+eps {
		t.Fatalf("expected BudgetTotal=1.0, got %f", b.BudgetTotal)
	}
	if b.BudgetConsumed < 1.0-eps || b.BudgetConsumed > 1.0+eps {
		t.Fatalf("expected BudgetConsumed=1.0, got %f", b.BudgetConsumed)
	}
	if b.BudgetRemaining < 0.0-eps || b.BudgetRemaining > 0.0+eps {
		t.Fatalf("expected BudgetRemaining=0.0, got %f", b.BudgetRemaining)
	}
	if b.Status != slo.BudgetExhausted {
		t.Fatalf("expected Status=%q, got %q", slo.BudgetExhausted, b.Status)
	}
	if b.SLOID != 1 {
		t.Fatalf("expected SLOID=1, got %d", b.SLOID)
	}
}

func TestEvaluateAll_SLOWithNoEvents_ZeroConsumption(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	reader := &stubSLOReader{
		listFunc: func(_ context.Context, _ string) ([]slo.Snapshot, error) {
			return []slo.Snapshot{
				{
					ID:                2,
					TenantID:          "tenant-a",
					Name:              "latency-slo",
					SLIID:             20,
					Target:            0.995,
					WindowType:        slo.WindowTypeRolling,
					WindowDuration:    7 * 24 * time.Hour,
					AlertFastBurnRate: 14.4,
					AlertSlowBurnRate: 2.0,
					Status:            slo.StatusActive,
				},
			}, nil
		},
	}
	agg := &stubSnapshotAggregator{
		aggregateFunc: func(_ context.Context, _ string, sliID int64, _, _ time.Time) (WindowAggregate, error) {
			return WindowAggregate{GoodEventsCount: 0, TotalEventsCount: 0}, nil
		},
	}
	bw := &stubBudgetWriter{}
	aw := &stubAlertWriter{}

	uc := NewEvaluateUseCase(reader, agg, bw, aw)
	uc.clock = func() time.Time { return now }

	err := uc.EvaluateAll(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("EvaluateAll() error = %v", err)
	}

	if len(bw.budgets) != 1 {
		t.Fatalf("expected 1 budget, got %d", len(bw.budgets))
	}
	b := bw.budgets[0]

	if b.BudgetTotal != 0 || b.BudgetConsumed != 0 || b.BudgetRemaining != 0 || b.BurnRate != 0 {
		t.Fatalf("expected all zeros for no events, got %+v", b)
	}
	if b.Status != slo.BudgetHealthy {
		t.Fatalf("expected Status=%q for zero events, got %q", slo.BudgetHealthy, b.Status)
	}
}

func TestEvaluateAll_FastBurnAlertTriggered(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	reader := &stubSLOReader{
		listFunc: func(_ context.Context, _ string) ([]slo.Snapshot, error) {
			return []slo.Snapshot{
				{
					ID:                3,
					TenantID:          "tenant-a",
					Name:              "fast-burn-slo",
					SLIID:             30,
					Target:            0.99,
					WindowType:        slo.WindowTypeRolling,
					WindowDuration:    30 * 24 * time.Hour,
					AlertFastBurnRate: 14.4,
					AlertSlowBurnRate: 2.0,
					Status:            slo.StatusActive,
				},
			}, nil
		},
	}
	// 50 good out of 100 -> high error rate, high burn rate
	agg := &stubSnapshotAggregator{
		aggregateFunc: func(_ context.Context, _ string, _ int64, _, _ time.Time) (WindowAggregate, error) {
			return WindowAggregate{GoodEventsCount: 50, TotalEventsCount: 100}, nil
		},
	}
	bw := &stubBudgetWriter{}
	aw := &stubAlertWriter{activeAlerts: nil}

	uc := NewEvaluateUseCase(reader, agg, bw, aw)
	uc.clock = func() time.Time { return now }

	err := uc.EvaluateAll(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("EvaluateAll() error = %v", err)
	}

	if len(aw.created) != 2 {
		t.Fatalf("expected 2 alerts created (fast + slow), got %d", len(aw.created))
	}
	if aw.created[0].alertType != "fast_burn" {
		t.Fatalf("expected first alert fast_burn, got %q", aw.created[0].alertType)
	}
	if aw.created[0].burnRate < 14.4 {
		t.Fatalf("expected burnRate >= 14.4, got %f", aw.created[0].burnRate)
	}
	if aw.created[1].alertType != "slow_burn" {
		t.Fatalf("expected second alert slow_burn, got %q", aw.created[1].alertType)
	}
}

func TestEvaluateAll_SlowBurnAlertTriggered(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	reader := &stubSLOReader{
		listFunc: func(_ context.Context, _ string) ([]slo.Snapshot, error) {
			return []slo.Snapshot{
				{
					ID:                4,
					TenantID:          "tenant-a",
					Name:              "slow-burn-slo",
					SLIID:             40,
					Target:            0.99,
					WindowType:        slo.WindowTypeRolling,
					WindowDuration:    30 * 24 * time.Hour,
					AlertFastBurnRate: 14.4,
					AlertSlowBurnRate: 2.0,
					Status:            slo.StatusActive,
				},
			}, nil
		},
	}
	// Burn rate should be between 2.0 and 14.4.
	// target=0.99, total=100, good=98 -> consumed=1, total=1, burnRate = 1 / (elapsed/window)
	// With elapsed=15 days, window=30 days: burnRate = 1 / 0.5 = 2.0
	// We need burnRate > 2.0 but < 14.4. Let's use good=97 so consumed=2, burnRate=4.0
	agg := &stubSnapshotAggregator{
		aggregateFunc: func(_ context.Context, _ string, _ int64, _, _ time.Time) (WindowAggregate, error) {
			return WindowAggregate{GoodEventsCount: 97, TotalEventsCount: 100}, nil
		},
	}
	bw := &stubBudgetWriter{}
	aw := &stubAlertWriter{activeAlerts: nil}

	uc := NewEvaluateUseCase(reader, agg, bw, aw)
	uc.clock = func() time.Time { return now }

	err := uc.EvaluateAll(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("EvaluateAll() error = %v", err)
	}

	if len(aw.created) != 1 {
		t.Fatalf("expected 1 alert created, got %d", len(aw.created))
	}
	if aw.created[0].alertType != "slow_burn" {
		t.Fatalf("expected slow_burn alert, got %q", aw.created[0].alertType)
	}
	if aw.created[0].burnRate < 2.0 || aw.created[0].burnRate >= 14.4 {
		t.Fatalf("expected burnRate in [2.0, 14.4), got %f", aw.created[0].burnRate)
	}
}

func TestEvaluateAll_FastBurnAlertResolved(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	reader := &stubSLOReader{
		listFunc: func(_ context.Context, _ string) ([]slo.Snapshot, error) {
			return []slo.Snapshot{
				{
					ID:                5,
					TenantID:          "tenant-a",
					Name:              "recovering-slo",
					SLIID:             50,
					Target:            0.99,
					WindowType:        slo.WindowTypeRolling,
					WindowDuration:    30 * 24 * time.Hour,
					AlertFastBurnRate: 14.4,
					AlertSlowBurnRate: 2.0,
					Status:            slo.StatusActive,
				},
			}, nil
		},
	}
	// All good -> burn rate = 0 < 14.4, should resolve existing fast burn alert
	agg := &stubSnapshotAggregator{
		aggregateFunc: func(_ context.Context, _ string, _ int64, _, _ time.Time) (WindowAggregate, error) {
			return WindowAggregate{GoodEventsCount: 100, TotalEventsCount: 100}, nil
		},
	}
	bw := &stubBudgetWriter{}
	aw := &stubAlertWriter{
		activeAlerts: []AlertSnapshot{
			{ID: 1, AlertType: "fast_burn", Status: "active"},
		},
	}

	uc := NewEvaluateUseCase(reader, agg, bw, aw)
	uc.clock = func() time.Time { return now }

	err := uc.EvaluateAll(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("EvaluateAll() error = %v", err)
	}

	if len(aw.resolved) != 1 {
		t.Fatalf("expected 1 alert resolved, got %d", len(aw.resolved))
	}
	if aw.resolved[0].sloID != 5 {
		t.Fatalf("expected resolve for sloID=5, got %d", aw.resolved[0].sloID)
	}
	if len(aw.created) != 0 {
		t.Fatalf("expected 0 alerts created, got %d", len(aw.created))
	}
}

func TestEvaluateAll_SlowBurnAlertResolved(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	reader := &stubSLOReader{
		listFunc: func(_ context.Context, _ string) ([]slo.Snapshot, error) {
			return []slo.Snapshot{
				{
					ID:                6,
					TenantID:          "tenant-a",
					Name:              "slow-recovering-slo",
					SLIID:             60,
					Target:            0.99,
					WindowType:        slo.WindowTypeRolling,
					WindowDuration:    30 * 24 * time.Hour,
					AlertFastBurnRate: 14.4,
					AlertSlowBurnRate: 2.0,
					Status:            slo.StatusActive,
				},
			}, nil
		},
	}
	agg := &stubSnapshotAggregator{
		aggregateFunc: func(_ context.Context, _ string, _ int64, _, _ time.Time) (WindowAggregate, error) {
			return WindowAggregate{GoodEventsCount: 100, TotalEventsCount: 100}, nil
		},
	}
	bw := &stubBudgetWriter{}
	aw := &stubAlertWriter{
		activeAlerts: []AlertSnapshot{
			{ID: 2, AlertType: "slow_burn", Status: "active"},
		},
	}

	uc := NewEvaluateUseCase(reader, agg, bw, aw)
	uc.clock = func() time.Time { return now }

	err := uc.EvaluateAll(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("EvaluateAll() error = %v", err)
	}

	if len(aw.resolved) != 1 {
		t.Fatalf("expected 1 alert resolved, got %d", len(aw.resolved))
	}
	if aw.resolved[0].sloID != 6 {
		t.Fatalf("expected resolve for sloID=6, got %d", aw.resolved[0].sloID)
	}
	if len(aw.created) != 0 {
		t.Fatalf("expected 0 alerts created, got %d", len(aw.created))
	}
}

func TestEvaluateAll_ReaderError(t *testing.T) {
	wantErr := errors.New("db down")
	reader := &stubSLOReader{
		listFunc: func(_ context.Context, _ string) ([]slo.Snapshot, error) {
			return nil, wantErr
		},
	}
	agg := &stubSnapshotAggregator{}
	bw := &stubBudgetWriter{}
	aw := &stubAlertWriter{}

	uc := NewEvaluateUseCase(reader, agg, bw, aw)
	err := uc.EvaluateAll(context.Background(), "tenant-a")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error wrapping %v, got %v", wantErr, err)
	}
}

func TestEvaluateAll_AggregatorError_Continues(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	reader := &stubSLOReader{
		listFunc: func(_ context.Context, _ string) ([]slo.Snapshot, error) {
			return []slo.Snapshot{
				{ID: 7, Name: "bad-slo", SLIID: 70, Target: 0.99, WindowType: slo.WindowTypeRolling, WindowDuration: 30 * 24 * time.Hour, AlertFastBurnRate: 14.4, AlertSlowBurnRate: 2.0, Status: slo.StatusActive},
				{ID: 8, Name: "good-slo", SLIID: 80, Target: 0.99, WindowType: slo.WindowTypeRolling, WindowDuration: 30 * 24 * time.Hour, AlertFastBurnRate: 14.4, AlertSlowBurnRate: 2.0, Status: slo.StatusActive},
			}, nil
		},
	}
	agg := &stubSnapshotAggregator{
		aggregateFunc: func(_ context.Context, _ string, sliID int64, _, _ time.Time) (WindowAggregate, error) {
			if sliID == 70 {
				return WindowAggregate{}, errors.New("aggregate failed")
			}
			return WindowAggregate{GoodEventsCount: 100, TotalEventsCount: 100}, nil
		},
	}
	bw := &stubBudgetWriter{}
	aw := &stubAlertWriter{}

	uc := NewEvaluateUseCase(reader, agg, bw, aw)
	uc.clock = func() time.Time { return now }

	// Errors are logged and continue; EvaluateAll returns nil.
	err := uc.EvaluateAll(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("EvaluateAll() error = %v, expected nil (errors continue)", err)
	}

	if len(bw.budgets) != 1 {
		t.Fatalf("expected 1 budget (for good slo), got %d", len(bw.budgets))
	}
	if bw.budgets[0].SLOID != 8 {
		t.Fatalf("expected budget for SLOID=8, got %d", bw.budgets[0].SLOID)
	}
}

func TestEvaluateSLO_NotFound(t *testing.T) {
	reader := &stubSLOReader{
		listFunc: func(_ context.Context, _ string) ([]slo.Snapshot, error) {
			return []slo.Snapshot{
				{ID: 1, Name: "other", SLIID: 10, Target: 0.99, WindowType: slo.WindowTypeRolling, WindowDuration: 30 * 24 * time.Hour, AlertFastBurnRate: 14.4, AlertSlowBurnRate: 2.0, Status: slo.StatusActive},
			}, nil
		},
	}
	agg := &stubSnapshotAggregator{}
	bw := &stubBudgetWriter{}
	aw := &stubAlertWriter{}

	uc := NewEvaluateUseCase(reader, agg, bw, aw)
	err := uc.EvaluateSLO(context.Background(), "tenant-a", 99)
	if err == nil {
		t.Fatalf("expected error for missing SLO, got nil")
	}
}

func TestEvaluateSLO_SingleSLO(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	reader := &stubSLOReader{
		listFunc: func(_ context.Context, _ string) ([]slo.Snapshot, error) {
			return []slo.Snapshot{
				{ID: 9, Name: "target-slo", SLIID: 90, Target: 0.99, WindowType: slo.WindowTypeRolling, WindowDuration: 30 * 24 * time.Hour, AlertFastBurnRate: 14.4, AlertSlowBurnRate: 2.0, Status: slo.StatusActive},
			}, nil
		},
	}
	agg := &stubSnapshotAggregator{
		aggregateFunc: func(_ context.Context, _ string, _ int64, _, _ time.Time) (WindowAggregate, error) {
			return WindowAggregate{GoodEventsCount: 100, TotalEventsCount: 100}, nil
		},
	}
	bw := &stubBudgetWriter{}
	aw := &stubAlertWriter{}

	uc := NewEvaluateUseCase(reader, agg, bw, aw)
	uc.clock = func() time.Time { return now }

	err := uc.EvaluateSLO(context.Background(), "tenant-a", 9)
	if err != nil {
		t.Fatalf("EvaluateSLO() error = %v", err)
	}

	if len(bw.budgets) != 1 {
		t.Fatalf("expected 1 budget, got %d", len(bw.budgets))
	}
	if bw.budgets[0].SLOID != 9 {
		t.Fatalf("expected budget for SLOID=9, got %d", bw.budgets[0].SLOID)
	}
}

func TestEvaluateAll_NoDuplicateFastBurnAlert(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	reader := &stubSLOReader{
		listFunc: func(_ context.Context, _ string) ([]slo.Snapshot, error) {
			return []slo.Snapshot{
				{
					ID:                10,
					TenantID:          "tenant-a",
					Name:              "already-alerted-slo",
					SLIID:             100,
					Target:            0.99,
					WindowType:        slo.WindowTypeRolling,
					WindowDuration:    30 * 24 * time.Hour,
					AlertFastBurnRate: 14.4,
					AlertSlowBurnRate: 2.0,
					Status:            slo.StatusActive,
				},
			}, nil
		},
	}
	agg := &stubSnapshotAggregator{
		aggregateFunc: func(_ context.Context, _ string, _ int64, _, _ time.Time) (WindowAggregate, error) {
			return WindowAggregate{GoodEventsCount: 50, TotalEventsCount: 100}, nil
		},
	}
	bw := &stubBudgetWriter{}
	aw := &stubAlertWriter{
		activeAlerts: []AlertSnapshot{
			{ID: 5, AlertType: "fast_burn", Status: "active"},
		},
	}

	uc := NewEvaluateUseCase(reader, agg, bw, aw)
	uc.clock = func() time.Time { return now }

	err := uc.EvaluateAll(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("EvaluateAll() error = %v", err)
	}

	// Fast burn already active, but slow burn is not — burn rate exceeds both thresholds.
	if len(aw.created) != 1 {
		t.Fatalf("expected 1 new slow_burn alert when fast burn already active, got %d", len(aw.created))
	}
	if aw.created[0].alertType != "slow_burn" {
		t.Fatalf("expected slow_burn alert, got %q", aw.created[0].alertType)
	}
	if len(aw.resolved) != 0 {
		t.Fatalf("expected 0 resolves when still in fast burn, got %d", len(aw.resolved))
	}
}

func TestEvaluateAll_NoDuplicateSlowBurnAlert(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	reader := &stubSLOReader{
		listFunc: func(_ context.Context, _ string) ([]slo.Snapshot, error) {
			return []slo.Snapshot{
				{
					ID:                11,
					TenantID:          "tenant-a",
					Name:              "already-slow-alerted-slo",
					SLIID:             110,
					Target:            0.99,
					WindowType:        slo.WindowTypeRolling,
					WindowDuration:    30 * 24 * time.Hour,
					AlertFastBurnRate: 14.4,
					AlertSlowBurnRate: 2.0,
					Status:            slo.StatusActive,
				},
			}, nil
		},
	}
	agg := &stubSnapshotAggregator{
		aggregateFunc: func(_ context.Context, _ string, _ int64, _, _ time.Time) (WindowAggregate, error) {
			return WindowAggregate{GoodEventsCount: 97, TotalEventsCount: 100}, nil
		},
	}
	bw := &stubBudgetWriter{}
	aw := &stubAlertWriter{
		activeAlerts: []AlertSnapshot{
			{ID: 6, AlertType: "slow_burn", Status: "active"},
		},
	}

	uc := NewEvaluateUseCase(reader, agg, bw, aw)
	uc.clock = func() time.Time { return now }

	err := uc.EvaluateAll(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("EvaluateAll() error = %v", err)
	}

	if len(aw.created) != 0 {
		t.Fatalf("expected 0 new alerts when slow burn already active, got %d", len(aw.created))
	}
	if len(aw.resolved) != 0 {
		t.Fatalf("expected 0 resolves when still in slow burn, got %d", len(aw.resolved))
	}
}

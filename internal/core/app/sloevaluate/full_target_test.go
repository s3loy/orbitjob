package sloevaluate

import (
	"context"
	"encoding/json"
	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/platform/scan"
	"testing"
	"time"
)

func TestFullTargetFailsOnObservedErrors(t *testing.T) {
	agg := &stubSnapshotAggregator{aggregateFunc: func(context.Context, string, int64, time.Time, time.Time) (WindowAggregate, error) {
		return WindowAggregate{TotalEventsCount: 100}, nil
	}}
	bw := &stubBudgetWriter{}
	aw := &stubAlertWriter{}
	uc := NewEvaluateUseCase(nil, agg, bw, aw)
	s := slo.Snapshot{ID: 1, SLIID: 1, Target: 1, WindowType: slo.WindowTypeRolling, WindowDuration: time.Hour, AlertFastBurnRate: 14.4, AlertSlowBurnRate: 2}
	if err := uc.evaluateSLO(context.Background(), scan.GenerateID(), s); err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(bw.budgets[0]); err != nil {
		t.Fatalf("exhausted 100%% budget cannot be serialized: %v", err)
	}
	if bw.budgets[0].Status != slo.BudgetExhausted || len(aw.created) != 2 {
		t.Fatalf("budget=%+v alerts=%+v", bw.budgets[0], aw.created)
	}
}

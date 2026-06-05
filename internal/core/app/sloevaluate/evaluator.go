package sloevaluate

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/platform/metrics"
)

// sloReader lists active SLOs.
type sloReader interface {
	ListActive(ctx context.Context, tenantID string) ([]slo.Snapshot, error)
}

// snapshotAggregator aggregates SLI snapshots over a time window.
type snapshotAggregator interface {
	AggregateWindow(ctx context.Context, tenantID string, sliID int64, start, end time.Time) (WindowAggregate, error)
}

// budgetWriter persists budget calculations.
type budgetWriter interface {
	Upsert(ctx context.Context, tenantID string, budget slo.Budget) (slo.Budget, error)
}

// alertWriter creates and resolves budget burn alerts.
type alertWriter interface {
	Create(ctx context.Context, tenantID string, sloID, budgetID int64, alertType string, burnRate float64) error
	ResolveBySLOAndType(ctx context.Context, tenantID string, sloID int64, alertType string) error
	GetActiveBySLO(ctx context.Context, tenantID string, sloID int64) ([]AlertSnapshot, error)
}

// WindowAggregate holds aggregated SLI data for a time window.
type WindowAggregate struct {
	GoodEventsCount  int64
	TotalEventsCount int64
	SLIValue         float64
}

// AlertSnapshot represents a budget alert.
type AlertSnapshot struct {
	ID        int64
	AlertType string
	Status    string
}

// EvaluateUseCase evaluates SLOs and manages burn rate alerts.
type EvaluateUseCase struct {
	sloReader      sloReader
	snapshotReader snapshotAggregator
	budgetWriter   budgetWriter
	alertWriter    alertWriter
	clock          func() time.Time
}

// NewEvaluateUseCase creates a new evaluator.
func NewEvaluateUseCase(
	sloReader sloReader,
	snapshotReader snapshotAggregator,
	budgetWriter budgetWriter,
	alertWriter alertWriter,
) *EvaluateUseCase {
	return &EvaluateUseCase{
		sloReader:      sloReader,
		snapshotReader: snapshotReader,
		budgetWriter:   budgetWriter,
		alertWriter:    alertWriter,
		clock:          func() time.Time { return time.Now().UTC() },
	}
}

// EvaluateAll evaluates all active SLOs for a tenant.
func (uc *EvaluateUseCase) EvaluateAll(ctx context.Context, tenantID string) error {
	start := time.Now()
	defer func() {
		metrics.SLOEvaluationDuration.Observe(time.Since(start).Seconds())
	}()

	slos, err := uc.sloReader.ListActive(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("list active slos: %w", err)
	}

	for _, s := range slos {
		if err := uc.evaluateSLO(ctx, tenantID, s); err != nil {
			slog.Error("failed to evaluate slo",
				"slo_id", s.ID,
				"slo_name", s.Name,
				"error", err,
			)
			continue
		}
	}

	return nil
}

// EvaluateSLO evaluates a single SLO.
func (uc *EvaluateUseCase) EvaluateSLO(ctx context.Context, tenantID string, sloID int64) error {
	slos, err := uc.sloReader.ListActive(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("list active slos: %w", err)
	}

	for _, s := range slos {
		if s.ID == sloID {
			return uc.evaluateSLO(ctx, tenantID, s)
		}
	}

	return fmt.Errorf("slo %d not found or not active", sloID)
}

func (uc *EvaluateUseCase) evaluateSLO(ctx context.Context, tenantID string, s slo.Snapshot) error {
	now := uc.clock()

	// 1. Calculate the current window.
	window := slo.CalculateWindow(s.WindowType, s.WindowDuration, now)

	// 2. Aggregate SLI snapshots for the window.
	aggregate, err := uc.snapshotReader.AggregateWindow(ctx, tenantID, s.SLIID, window.Start, window.End)
	if err != nil {
		return fmt.Errorf("aggregate window for sli %d: %w", s.SLIID, err)
	}

	if aggregate.TotalEventsCount == 0 {
		// No events yet, still update budget with zero consumption.
		budget := slo.Budget{
			TenantID:        tenantID,
			SLOID:           s.ID,
			WindowStart:     window.Start,
			WindowEnd:       window.End,
			BudgetTotal:     0,
			BudgetConsumed:  0,
			BudgetRemaining: 0,
			BurnRate:        0,
			Status:          slo.BudgetHealthy,
		}
		if _, err := uc.budgetWriter.Upsert(ctx, tenantID, budget); err != nil {
			return fmt.Errorf("upsert budget: %w", err)
		}
		return nil
	}

	// 3. Calculate SLI value.
	sliValue := float64(aggregate.GoodEventsCount) / float64(aggregate.TotalEventsCount)

	// 4. Calculate error budget.
	// Budget total = allowed errors = (1 - target) * total_events
	budgetTotal := (1.0 - s.Target) * float64(aggregate.TotalEventsCount)
	// Budget consumed = actual errors = (1 - sli_value) * total_events
	budgetConsumed := (1.0 - sliValue) * float64(aggregate.TotalEventsCount)
	budgetRemaining := budgetTotal - budgetConsumed

	// 5. Calculate burn rate.
	elapsed := window.Elapsed(now)
	burnRate := slo.CalculateBurnRate(budgetTotal, budgetConsumed, elapsed, window.Duration())

	// 6. Determine budget status.
	status := slo.BudgetStatus(budgetRemaining, budgetTotal)

	// 7. Persist budget.
	budget := slo.Budget{
		TenantID:        tenantID,
		SLOID:           s.ID,
		WindowStart:     window.Start,
		WindowEnd:       window.End,
		BudgetTotal:     budgetTotal,
		BudgetConsumed:  budgetConsumed,
		BudgetRemaining: budgetRemaining,
		BurnRate:        burnRate,
		Status:          status,
	}
	budget, err = uc.budgetWriter.Upsert(ctx, tenantID, budget)
	if err != nil {
		return fmt.Errorf("upsert budget: %w", err)
	}

	// Emit metrics.
	sloIDStr := strconv.FormatInt(s.ID, 10)
	metrics.SLOBudgetBurnRate.WithLabelValues(tenantID, sloIDStr).Set(burnRate)
	statusValue := 0.0
	switch status {
	case slo.BudgetAtRisk:
		statusValue = 1.0
	case slo.BudgetExhausted:
		statusValue = 2.0
	}
	metrics.SLOBudgetStatus.WithLabelValues(tenantID, sloIDStr).Set(statusValue)

	// 8. Check burn rate thresholds and manage alerts.
	if err := uc.manageAlerts(ctx, tenantID, s, budget); err != nil {
		return fmt.Errorf("manage alerts: %w", err)
	}

	return nil
}

func (uc *EvaluateUseCase) manageAlerts(ctx context.Context, tenantID string, s slo.Snapshot, budget slo.Budget) error {
	// Get existing active alerts.
	existingAlerts, err := uc.alertWriter.GetActiveBySLO(ctx, tenantID, s.ID)
	if err != nil {
		return fmt.Errorf("get active alerts: %w", err)
	}

	hasFastBurn := false
	hasSlowBurn := false
	for _, a := range existingAlerts {
		if a.AlertType == "fast_burn" {
			hasFastBurn = true
		}
		if a.AlertType == "slow_burn" {
			hasSlowBurn = true
		}
	}

	// Check fast burn rate threshold.
	if budget.BurnRate >= s.AlertFastBurnRate {
		if !hasFastBurn {
			if err := uc.alertWriter.Create(ctx, tenantID, s.ID, budget.ID, "fast_burn", budget.BurnRate); err != nil {
				return fmt.Errorf("create fast burn alert: %w", err)
			}
			metrics.SLOBudgetAlertsTotal.WithLabelValues(tenantID, "fast_burn").Inc()
			slog.Warn("fast burn rate alert triggered",
				"slo_id", s.ID,
				"slo_name", s.Name,
				"burn_rate", budget.BurnRate,
			)
		}
	} else if hasFastBurn {
		// Fast burn resolved.
		if err := uc.alertWriter.ResolveBySLOAndType(ctx, tenantID, s.ID, "fast_burn"); err != nil {
			return fmt.Errorf("resolve fast burn alert: %w", err)
		}
	}

	// Check slow burn rate threshold independently.
	if budget.BurnRate >= s.AlertSlowBurnRate {
		if !hasSlowBurn {
			if err := uc.alertWriter.Create(ctx, tenantID, s.ID, budget.ID, "slow_burn", budget.BurnRate); err != nil {
				return fmt.Errorf("create slow burn alert: %w", err)
			}
			metrics.SLOBudgetAlertsTotal.WithLabelValues(tenantID, "slow_burn").Inc()
			slog.Warn("slow burn rate alert triggered",
				"slo_id", s.ID,
				"slo_name", s.Name,
				"burn_rate", budget.BurnRate,
			)
		}
	} else if hasSlowBurn {
		// Slow burn resolved.
		if err := uc.alertWriter.ResolveBySLOAndType(ctx, tenantID, s.ID, "slow_burn"); err != nil {
			return fmt.Errorf("resolve slow burn alert: %w", err)
		}
	}

	return nil
}

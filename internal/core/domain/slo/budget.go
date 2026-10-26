package slo

import (
	"math"
	"time"
)

// MaxBurnRate fits the persisted DECIMAL(10,4) column; infinity cannot be stored.
const MaxBurnRate = 999999.0

// Budget represents the error budget for an SLO within a time window.
type Budget struct {
	ID              int64
	TenantID        string
	SLOID           int64
	WindowStart     time.Time
	WindowEnd       time.Time
	BudgetTotal     float64
	BudgetConsumed  float64
	BudgetRemaining float64
	BurnRate        float64
	Status          string
	Version         int
}

// CalculateBurnRate computes the burn rate given budget consumption and elapsed time.
//
// Burn Rate = (budget_consumed / budget_total) / (elapsed_time / window_duration)
//
// A burn rate of 1.0 means the budget is being consumed exactly as expected.
// A burn rate of 14.4 means 2% of the budget will be consumed in 1 hour.
func CalculateBurnRate(budgetTotal, budgetConsumed float64, elapsed, windowDuration time.Duration) float64 {
	if budgetTotal == 0 && budgetConsumed > 0 {
		return MaxBurnRate
	}
	if budgetTotal <= 0 || elapsed <= 0 || windowDuration <= 0 {
		return 0
	}
	budgetRatio := budgetConsumed / budgetTotal
	elapsedRatio := float64(elapsed) / float64(windowDuration)
	if elapsedRatio <= 0 {
		return 0
	}
	rate := budgetRatio / elapsedRatio
	if math.IsNaN(rate) {
		return 0
	}
	if rate > MaxBurnRate {
		return MaxBurnRate
	}
	return rate
}

// BudgetStatus determines the budget status based on remaining budget.
func BudgetStatus(budgetRemaining, budgetTotal float64) string {
	if budgetTotal == 0 && budgetRemaining < 0 {
		return BudgetExhausted
	}
	if budgetTotal <= 0 {
		return BudgetHealthy
	}
	if budgetRemaining <= 0 {
		return BudgetExhausted
	}
	if budgetRemaining/budgetTotal < 0.1 {
		return BudgetAtRisk
	}
	return BudgetHealthy
}

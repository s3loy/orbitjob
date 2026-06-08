package slo

import (
	"testing"
	"time"
)

func TestCalculateBurnRate(t *testing.T) {
	tests := []struct {
		name           string
		budgetTotal    float64
		budgetConsumed float64
		elapsed        time.Duration
		windowDuration time.Duration
		want           float64
	}{
		{
			name:           "exactly_on_schedule",
			budgetTotal:    100,
			budgetConsumed: 50,
			elapsed:        12 * time.Hour,
			windowDuration: 24 * time.Hour,
			want:           1.0,
		},
		{
			name:           "double_speed",
			budgetTotal:    100,
			budgetConsumed: 50,
			elapsed:        6 * time.Hour,
			windowDuration: 24 * time.Hour,
			want:           2.0,
		},
		{
			name:           "half_speed",
			budgetTotal:    100,
			budgetConsumed: 25,
			elapsed:        12 * time.Hour,
			windowDuration: 24 * time.Hour,
			want:           0.5,
		},
		{
			name:           "zero_budget_total",
			budgetTotal:    0,
			budgetConsumed: 50,
			elapsed:        12 * time.Hour,
			windowDuration: 24 * time.Hour,
			want:           0,
		},
		{
			name:           "negative_budget_total",
			budgetTotal:    -10,
			budgetConsumed: 5,
			elapsed:        12 * time.Hour,
			windowDuration: 24 * time.Hour,
			want:           0,
		},
		{
			name:           "zero_elapsed",
			budgetTotal:    100,
			budgetConsumed: 0,
			elapsed:        0,
			windowDuration: 24 * time.Hour,
			want:           0,
		},
		{
			name:           "negative_elapsed",
			budgetTotal:    100,
			budgetConsumed: 10,
			elapsed:        -1 * time.Hour,
			windowDuration: 24 * time.Hour,
			want:           0,
		},
		{
			name:           "zero_window_duration",
			budgetTotal:    100,
			budgetConsumed: 10,
			elapsed:        1 * time.Hour,
			windowDuration: 0,
			want:           0,
		},
		{
			name:           "negative_window_duration",
			budgetTotal:    100,
			budgetConsumed: 10,
			elapsed:        1 * time.Hour,
			windowDuration: -1 * time.Hour,
			want:           0,
		},
		{
			name:           "no_consumption",
			budgetTotal:    100,
			budgetConsumed: 0,
			elapsed:        12 * time.Hour,
			windowDuration: 24 * time.Hour,
			want:           0,
		},
		{
			name:           "full_consumption",
			budgetTotal:    100,
			budgetConsumed: 100,
			elapsed:        24 * time.Hour,
			windowDuration: 24 * time.Hour,
			want:           1.0,
		},
		{
			name:           "over_consumption",
			budgetTotal:    100,
			budgetConsumed: 150,
			elapsed:        12 * time.Hour,
			windowDuration: 24 * time.Hour,
			want:           3.0,
		},
		{
			name:           "fast_burn_rate",
			budgetTotal:    100,
			budgetConsumed: 2,
			elapsed:        1 * time.Hour,
			windowDuration: 24 * time.Hour,
			want:           0.48,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateBurnRate(tt.budgetTotal, tt.budgetConsumed, tt.elapsed, tt.windowDuration)
			// Use small epsilon for float comparison
			const epsilon = 0.0001
			if diff := got - tt.want; diff < -epsilon || diff > epsilon {
				t.Errorf("CalculateBurnRate() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestBudgetStatus(t *testing.T) {
	tests := []struct {
		name            string
		budgetRemaining float64
		budgetTotal     float64
		want            string
	}{
		{
			name:            "healthy_plenty_remaining",
			budgetRemaining: 50,
			budgetTotal:     100,
			want:            BudgetHealthy,
		},
		{
			name:            "healthy_exactly_at_threshold",
			budgetRemaining: 10,
			budgetTotal:     100,
			want:            BudgetHealthy,
		},
		{
			name:            "at_risk_below_threshold",
			budgetRemaining: 9.99,
			budgetTotal:     100,
			want:            BudgetAtRisk,
		},
		{
			name:            "at_risk_small_remaining",
			budgetRemaining: 1,
			budgetTotal:     100,
			want:            BudgetAtRisk,
		},
		{
			name:            "exhausted_zero_remaining",
			budgetRemaining: 0,
			budgetTotal:     100,
			want:            BudgetExhausted,
		},
		{
			name:            "exhausted_negative_remaining",
			budgetRemaining: -10,
			budgetTotal:     100,
			want:            BudgetExhausted,
		},
		{
			name:            "zero_budget_total",
			budgetRemaining: 0,
			budgetTotal:     0,
			want:            BudgetHealthy,
		},
		{
			name:            "negative_budget_total",
			budgetRemaining: -10,
			budgetTotal:     -100,
			want:            BudgetHealthy,
		},
		{
			name:            "remaining_greater_than_total",
			budgetRemaining: 150,
			budgetTotal:     100,
			want:            BudgetHealthy,
		},
		{
			name:            "at_risk_exactly_10_percent",
			budgetRemaining: 10,
			budgetTotal:     100,
			want:            BudgetHealthy,
		},
		{
			name:            "at_risk_just_under_10_percent",
			budgetRemaining: 9.999,
			budgetTotal:     100,
			want:            BudgetAtRisk,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BudgetStatus(tt.budgetRemaining, tt.budgetTotal)
			if got != tt.want {
				t.Errorf("BudgetStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

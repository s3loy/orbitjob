package query

import (
	"context"
	"fmt"

	"orbitjob/internal/core/domain/slo"
)

// budgetReader retrieves current budgets.
type budgetReader interface {
	GetCurrent(ctx context.Context, tenantID string, sloID int64) (slo.Budget, error)
}

// GetBudgetUseCase handles budget retrieval.
type GetBudgetUseCase struct {
	repo budgetReader
}

// NewGetBudgetUseCase creates a new use case.
func NewGetBudgetUseCase(repo budgetReader) *GetBudgetUseCase {
	return &GetBudgetUseCase{repo: repo}
}

// GetInput contains the parameters for retrieval.
type GetInput struct {
	SLOID int64
}

// GetItem is the read model for a budget.
type GetItem struct {
	WindowStart     string  `json:"window_start"`
	WindowEnd       string  `json:"window_end"`
	BudgetTotal     float64 `json:"budget_total"`
	BudgetConsumed  float64 `json:"budget_consumed"`
	BudgetRemaining float64 `json:"budget_remaining"`
	BurnRate        float64 `json:"burn_rate"`
	Status          string  `json:"status"`
	Version         int     `json:"version"`
}

// Get retrieves the current budget for an SLO.
func (uc *GetBudgetUseCase) Get(ctx context.Context, tenantID string, sloID int64) (GetItem, error) {
	if sloID <= 0 {
		return GetItem{}, fmt.Errorf("slo_id must be positive")
	}

	budget, err := uc.repo.GetCurrent(ctx, tenantID, sloID)
	if err != nil {
		return GetItem{}, err
	}

	return GetItem{
		WindowStart:     budget.WindowStart.Format("2006-01-02T15:04:05Z"),
		WindowEnd:       budget.WindowEnd.Format("2006-01-02T15:04:05Z"),
		BudgetTotal:     budget.BudgetTotal,
		BudgetConsumed:  budget.BudgetConsumed,
		BudgetRemaining: budget.BudgetRemaining,
		BurnRate:        budget.BurnRate,
		Status:          budget.Status,
		Version:         budget.Version,
	}, nil
}
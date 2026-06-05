package query

import (
	"context"

	"orbitjob/internal/core/domain/slo"
)

// budgetLister lists historical budgets.
type budgetLister interface {
	ListHistory(ctx context.Context, tenantID string, sloID int64, limit, offset int) ([]slo.Budget, int64, error)
}

// ListBudgetHistoryUseCase handles budget history listing.
type ListBudgetHistoryUseCase struct {
	repo budgetLister
}

// NewListBudgetHistoryUseCase creates a new use case.
func NewListBudgetHistoryUseCase(repo budgetLister) *ListBudgetHistoryUseCase {
	return &ListBudgetHistoryUseCase{repo: repo}
}

// ListInput contains the parameters for listing.
type ListInput struct {
	TenantID string
	SLOID    int64
	Limit    int
	Offset   int
}

// ListItem is a single item in the list.
type ListItem struct {
	WindowStart     string  `json:"window_start"`
	WindowEnd       string  `json:"window_end"`
	BudgetTotal     float64 `json:"budget_total"`
	BudgetConsumed  float64 `json:"budget_consumed"`
	BudgetRemaining float64 `json:"budget_remaining"`
	BurnRate        float64 `json:"burn_rate"`
	Status          string  `json:"status"`
}

// ListResult is the output of listing budgets.
type ListResult struct {
	Items  []ListItem `json:"items"`
	Total  int64      `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

// List retrieves a paginated list of budget history.
func (uc *ListBudgetHistoryUseCase) List(ctx context.Context, in ListInput) (ListResult, error) {
	if in.Limit <= 0 {
		in.Limit = 50
	}
	if in.Limit > 100 {
		in.Limit = 100
	}

	budgets, total, err := uc.repo.ListHistory(ctx, in.TenantID, in.SLOID, in.Limit, in.Offset)
	if err != nil {
		return ListResult{}, err
	}

	items := make([]ListItem, len(budgets))
	for i, b := range budgets {
		items[i] = ListItem{
			WindowStart:     b.WindowStart.Format("2006-01-02T15:04:05Z"),
			WindowEnd:       b.WindowEnd.Format("2006-01-02T15:04:05Z"),
			BudgetTotal:     b.BudgetTotal,
			BudgetConsumed:  b.BudgetConsumed,
			BudgetRemaining: b.BudgetRemaining,
			BurnRate:        b.BurnRate,
			Status:          b.Status,
		}
	}

	return ListResult{
		Items:  items,
		Total:  total,
		Limit:  in.Limit,
		Offset: in.Offset,
	}, nil
}

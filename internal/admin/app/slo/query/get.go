package query

import (
	"context"

	"orbitjob/internal/core/domain/slo"
)

// sloReader retrieves SLOs.
type sloReader interface {
	Get(ctx context.Context, tenantID string, id int64) (slo.Snapshot, error)
}

// budgetReader retrieves current budgets.
type budgetReader interface {
	GetCurrent(ctx context.Context, tenantID string, sloID int64) (slo.Budget, error)
}

// GetSLOUseCase handles SLO retrieval with current budget.
type GetSLOUseCase struct {
	sloRepo    sloReader
	budgetRepo budgetReader
}

// NewGetSLOUseCase creates a new use case.
func NewGetSLOUseCase(sloRepo sloReader, budgetRepo budgetReader) *GetSLOUseCase {
	return &GetSLOUseCase{sloRepo: sloRepo, budgetRepo: budgetRepo}
}

// GetInput contains the parameters for retrieval.
type GetInput struct {
	ID int64
}

// GetItem is the read model for a single SLO.
type GetItem struct {
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	Description       *string `json:"description,omitempty"`
	SLIID             int64   `json:"sli_id"`
	Target            float64 `json:"target"`
	WindowType        string  `json:"window_type"`
	WindowDuration    string  `json:"window_duration"`
	AlertFastBurnRate float64 `json:"alert_fast_burn_rate"`
	AlertSlowBurnRate float64 `json:"alert_slow_burn_rate"`
	Status            string  `json:"status"`
	Version           int     `json:"version"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`

	// Current budget (if available).
	CurrentBudget *BudgetItem `json:"current_budget,omitempty"`
}

// BudgetItem represents the current budget in the SLO response.
type BudgetItem struct {
	WindowStart     string  `json:"window_start"`
	WindowEnd       string  `json:"window_end"`
	BudgetTotal     float64 `json:"budget_total"`
	BudgetConsumed  float64 `json:"budget_consumed"`
	BudgetRemaining float64 `json:"budget_remaining"`
	BurnRate        float64 `json:"burn_rate"`
	Status          string  `json:"status"`
}

// Get retrieves a single SLO with its current budget.
func (uc *GetSLOUseCase) Get(ctx context.Context, tenantID string, id int64) (GetItem, error) {
	snap, err := uc.sloRepo.Get(ctx, tenantID, id)
	if err != nil {
		return GetItem{}, err
	}

	item := GetItem{
		ID:                snap.ID,
		Name:              snap.Name,
		Description:       snap.Description,
		SLIID:             snap.SLIID,
		Target:            snap.Target,
		WindowType:        snap.WindowType,
		WindowDuration:    snap.WindowDuration.String(),
		AlertFastBurnRate: snap.AlertFastBurnRate,
		AlertSlowBurnRate: snap.AlertSlowBurnRate,
		Status:            snap.Status,
		Version:           snap.Version,
		CreatedAt:         snap.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:         snap.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}

	// Try to get current budget (may not exist yet).
	budget, err := uc.budgetRepo.GetCurrent(ctx, tenantID, snap.ID)
	if err == nil {
		item.CurrentBudget = &BudgetItem{
			WindowStart:     budget.WindowStart.Format("2006-01-02T15:04:05Z"),
			WindowEnd:       budget.WindowEnd.Format("2006-01-02T15:04:05Z"),
			BudgetTotal:     budget.BudgetTotal,
			BudgetConsumed:  budget.BudgetConsumed,
			BudgetRemaining: budget.BudgetRemaining,
			BurnRate:        budget.BurnRate,
			Status:          budget.Status,
		}
	}

	return item, nil
}

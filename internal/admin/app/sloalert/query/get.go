package query

import (
	"context"
	"fmt"

	"orbitjob/internal/admin/store/postgres"
)

// alertReader retrieves budget alerts.
type alertReader interface {
	Get(ctx context.Context, tenantID string, id int64) (postgres.BudgetAlertItem, error)
}

// GetAlertUseCase handles alert retrieval.
type GetAlertUseCase struct {
	repo alertReader
}

// NewGetAlertUseCase creates a new use case.
func NewGetAlertUseCase(repo alertReader) *GetAlertUseCase {
	return &GetAlertUseCase{repo: repo}
}

// GetItem is the read model for a single alert.
type GetItem struct {
	ID          int64   `json:"id"`
	SLOID       int64   `json:"slo_id"`
	BudgetID    int64   `json:"budget_id"`
	AlertType   string  `json:"alert_type"`
	BurnRate    float64 `json:"burn_rate"`
	Status      string  `json:"status"`
	TriggeredAt string  `json:"triggered_at"`
	ResolvedAt  *string `json:"resolved_at,omitempty"`
}

// Get retrieves a single budget alert.
func (uc *GetAlertUseCase) Get(ctx context.Context, tenantID string, id int64) (GetItem, error) {
	if id <= 0 {
		return GetItem{}, fmt.Errorf("id must be positive")
	}

	item, err := uc.repo.Get(ctx, tenantID, id)
	if err != nil {
		return GetItem{}, err
	}

	result := GetItem{
		ID:          item.ID,
		SLOID:       item.SLOID,
		BudgetID:    item.BudgetID,
		AlertType:   item.AlertType,
		BurnRate:    item.BurnRate,
		Status:      item.Status,
		TriggeredAt: item.TriggeredAt.Format("2006-01-02T15:04:05Z"),
	}
	if item.ResolvedAt != nil {
		ra := item.ResolvedAt.Format("2006-01-02T15:04:05Z")
		result.ResolvedAt = &ra
	}

	return result, nil
}

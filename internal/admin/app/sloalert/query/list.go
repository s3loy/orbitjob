package query

import (
	"context"

	"orbitjob/internal/admin/store/postgres"
)

// alertLister lists budget alerts.
type alertLister interface {
	List(ctx context.Context, tenantID string, sloID *int64, status *string, limit, offset int) ([]postgres.BudgetAlertItem, int64, error)
}

// ListAlertsUseCase handles alert listing.
type ListAlertsUseCase struct {
	repo alertLister
}

// NewListAlertsUseCase creates a new use case.
func NewListAlertsUseCase(repo alertLister) *ListAlertsUseCase {
	return &ListAlertsUseCase{repo: repo}
}

// ListInput contains the parameters for listing.
type ListInput struct {
	TenantID string
	SLOID    *int64
	Status   *string
	Limit    int
	Offset   int
}

// ListItem is a single item in the list.
type ListItem struct {
	ID          int64   `json:"id"`
	SLOID       int64   `json:"slo_id"`
	AlertType   string  `json:"alert_type"`
	BurnRate    float64 `json:"burn_rate"`
	Status      string  `json:"status"`
	TriggeredAt string  `json:"triggered_at"`
}

// ListResult is the output of listing alerts.
type ListResult struct {
	Items  []ListItem `json:"items"`
	Total  int64      `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

// List retrieves a paginated list of budget alerts.
func (uc *ListAlertsUseCase) List(ctx context.Context, in ListInput) (ListResult, error) {
	if in.Limit <= 0 {
		in.Limit = 50
	}
	if in.Limit > 100 {
		in.Limit = 100
	}

	alerts, total, err := uc.repo.List(ctx, in.TenantID, in.SLOID, in.Status, in.Limit, in.Offset)
	if err != nil {
		return ListResult{}, err
	}

	items := make([]ListItem, len(alerts))
	for i, a := range alerts {
		items[i] = ListItem{
			ID:          a.ID,
			SLOID:       a.SLOID,
			AlertType:   a.AlertType,
			BurnRate:    a.BurnRate,
			Status:      a.Status,
			TriggeredAt: a.TriggeredAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	return ListResult{
		Items:  items,
		Total:  total,
		Limit:  in.Limit,
		Offset: in.Offset,
	}, nil
}

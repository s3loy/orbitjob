package query

import (
	"context"

	"orbitjob/internal/core/domain/slo"
)

// sloLister lists SLOs.
type sloLister interface {
	List(ctx context.Context, tenantID string, limit, offset int) ([]slo.Snapshot, int64, error)
}

// ListSLOsUseCase handles SLO listing.
type ListSLOsUseCase struct {
	repo sloLister
}

// NewListSLOsUseCase creates a new use case.
func NewListSLOsUseCase(repo sloLister) *ListSLOsUseCase {
	return &ListSLOsUseCase{repo: repo}
}

// ListInput contains the parameters for listing.
type ListInput struct {
	TenantID string
	Limit    int
	Offset   int
}

// ListItem is a single item in the list.
type ListItem struct {
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	SLIID             int64   `json:"sli_id"`
	Target            float64 `json:"target"`
	WindowType        string  `json:"window_type"`
	WindowDuration    string  `json:"window_duration"`
	AlertFastBurnRate float64 `json:"alert_fast_burn_rate"`
	AlertSlowBurnRate float64 `json:"alert_slow_burn_rate"`
	Status            string  `json:"status"`
	Version           int     `json:"version"`
	CreatedAt         string  `json:"created_at"`
}

// ListResult is the output of listing SLOs.
type ListResult struct {
	Items  []ListItem `json:"items"`
	Total  int64      `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

// List retrieves a paginated list of SLOs.
func (uc *ListSLOsUseCase) List(ctx context.Context, in ListInput) (ListResult, error) {
	if in.Limit <= 0 {
		in.Limit = 50
	}
	if in.Limit > 100 {
		in.Limit = 100
	}

	slos, total, err := uc.repo.List(ctx, in.TenantID, in.Limit, in.Offset)
	if err != nil {
		return ListResult{}, err
	}

	items := make([]ListItem, len(slos))
	for i, s := range slos {
		items[i] = ListItem{
			ID:                s.ID,
			Name:              s.Name,
			SLIID:             s.SLIID,
			Target:            s.Target,
			WindowType:        s.WindowType,
			WindowDuration:    s.WindowDuration.String(),
			AlertFastBurnRate: s.AlertFastBurnRate,
			AlertSlowBurnRate: s.AlertSlowBurnRate,
			Status:            s.Status,
			Version:           s.Version,
			CreatedAt:         s.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	return ListResult{
		Items:  items,
		Total:  total,
		Limit:  in.Limit,
		Offset: in.Offset,
	}, nil
}

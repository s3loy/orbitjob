package query

import (
	"context"

	"orbitjob/internal/core/domain/sli"
)

// sliLister lists SLIs.
type sliLister interface {
	List(ctx context.Context, tenantID string, limit, offset int) ([]sli.Snapshot, int64, error)
}

// ListSLIsUseCase handles SLI listing.
type ListSLIsUseCase struct {
	repo sliLister
}

// NewListSLIsUseCase creates a new use case.
func NewListSLIsUseCase(repo sliLister) *ListSLIsUseCase {
	return &ListSLIsUseCase{repo: repo}
}

// ListInput contains the parameters for listing.
type ListInput struct {
	TenantID string
	Limit    int
	Offset   int
}

// ListItem is a single item in the list.
type ListItem struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	SLIType     string `json:"sli_type"`
	SourceType  string `json:"source_type"`
	Aggregation string `json:"aggregation"`
	Version     int    `json:"version"`
	CreatedAt   string `json:"created_at"`
}

// ListResult is the output of listing SLIs.
type ListResult struct {
	Items  []ListItem `json:"items"`
	Total  int64      `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

// List retrieves a paginated list of SLIs.
func (uc *ListSLIsUseCase) List(ctx context.Context, in ListInput) (ListResult, error) {
	if in.Limit <= 0 {
		in.Limit = 50
	}
	if in.Limit > 100 {
		in.Limit = 100
	}

	slis, total, err := uc.repo.List(ctx, in.TenantID, in.Limit, in.Offset)
	if err != nil {
		return ListResult{}, err
	}

	items := make([]ListItem, len(slis))
	for i, s := range slis {
		items[i] = ListItem{
			ID:          s.ID,
			Name:        s.Name,
			SLIType:     s.SLIType,
			SourceType:  s.SourceType,
			Aggregation: s.Aggregation,
			Version:     s.Version,
			CreatedAt:   s.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	return ListResult{
		Items:  items,
		Total:  total,
		Limit:  in.Limit,
		Offset: in.Offset,
	}, nil
}

package query

import (
	"context"
	"time"
)

type checkRunLister interface {
	List(ctx context.Context, in ListCheckRunsInput) ([]ListItem, int, error)
}

type ListCheckRunsInput struct {
	TenantID string
	CheckID  *int64
	Status   *string
	Limit    int
	Offset   int
}

// ListItem represents a single check run in the list response.
type ListItem struct {
	ID         int64     `json:"id"`
	RunID      string    `json:"run_id"`
	CheckID    int64     `json:"check_id"`
	Status     string    `json:"status"`
	Severity   *string   `json:"severity,omitempty"`
	DurationMs *int      `json:"duration_ms,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type ListCheckRunsResult struct {
	Items []ListItem `json:"items"`
	Total int        `json:"total"`
}

type ListCheckRunsUseCase struct {
	repo checkRunLister
}

func NewListCheckRunsUseCase(repo checkRunLister) *ListCheckRunsUseCase {
	return &ListCheckRunsUseCase{repo: repo}
}

func (uc *ListCheckRunsUseCase) List(ctx context.Context, in ListCheckRunsInput) (ListCheckRunsResult, error) {
	items, total, err := uc.repo.List(ctx, in)
	if err != nil {
		return ListCheckRunsResult{}, err
	}
	return ListCheckRunsResult{Items: items, Total: total}, nil
}

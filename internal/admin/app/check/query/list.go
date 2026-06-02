package query

import (
	"context"
	"time"
)

type checkLister interface {
	List(ctx context.Context, in ListChecksInput) ([]ListItem, int, error)
}

type ListChecksInput struct {
	TenantID string
	Status   *string
	Limit    int
	Offset   int
}

// ListItem represents a single check in the list response.
type ListItem struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Description  *string    `json:"description,omitempty"`
	TenantID     string     `json:"tenant_id"`
	Status       string     `json:"status"`
	CheckType    string     `json:"check_type"`
	ScheduleType string     `json:"schedule_type"`
	NextRunAt    *time.Time `json:"next_run_at,omitempty"`
	Version      int        `json:"version"`
	CreatedAt    time.Time  `json:"created_at"`
}

type ListChecksResult struct {
	Items []ListItem `json:"items"`
	Total int        `json:"total"`
}

type ListChecksUseCase struct {
	repo checkLister
}

func NewListChecksUseCase(repo checkLister) *ListChecksUseCase {
	return &ListChecksUseCase{repo: repo}
}

func (uc *ListChecksUseCase) List(ctx context.Context, in ListChecksInput) (ListChecksResult, error) {
	items, total, err := uc.repo.List(ctx, in)
	if err != nil {
		return ListChecksResult{}, err
	}
	return ListChecksResult{Items: items, Total: total}, nil
}

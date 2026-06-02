package query

import (
	"context"
	"time"
)

type checkRunGetter interface {
	Get(ctx context.Context, tenantID string, id int64) (GetResult, error)
}

// GetResult is the result of getting a single check run.
type GetResult struct {
	ID               int64          `json:"id"`
	RunID            string         `json:"run_id"`
	TenantID         string         `json:"tenant_id"`
	CheckID          int64          `json:"check_id"`
	Status           string         `json:"status"`
	Severity         *string        `json:"severity,omitempty"`
	Output           map[string]any `json:"output"`
	EvaluationResult map[string]any `json:"evaluation_result"`
	ScheduledAt      time.Time      `json:"scheduled_at"`
	StartedAt        *time.Time     `json:"started_at,omitempty"`
	FinishedAt       *time.Time     `json:"finished_at,omitempty"`
	DurationMs       *int           `json:"duration_ms,omitempty"`
	Version          int            `json:"version"`
	CreatedAt        time.Time      `json:"created_at"`
}

type GetCheckRunUseCase struct {
	repo checkRunGetter
}

func NewGetCheckRunUseCase(repo checkRunGetter) *GetCheckRunUseCase {
	return &GetCheckRunUseCase{repo: repo}
}

func (uc *GetCheckRunUseCase) Get(ctx context.Context, tenantID string, id int64) (GetResult, error) {
	return uc.repo.Get(ctx, tenantID, id)
}

package query

import (
	"context"
	"time"
)

type attemptLister interface {
	ListAttempts(ctx context.Context, tenantID, runID string) ([]AttemptItem, error)
}

type ListAttemptsUseCase struct{ repo attemptLister }

func NewListAttemptsUseCase(repo attemptLister) *ListAttemptsUseCase {
	return &ListAttemptsUseCase{repo: repo}
}

type AttemptItem struct {
	AttemptNo  int        `json:"attempt_no"`
	WorkerID   *string    `json:"worker_id"`
	Status     string     `json:"status"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	ResultCode *string    `json:"result_code"`
	ErrorMsg   *string    `json:"error_msg"`
}

func (uc *ListAttemptsUseCase) List(ctx context.Context, tenantID, runID string) ([]AttemptItem, error) {
	return uc.repo.ListAttempts(ctx, tenantID, runID)
}

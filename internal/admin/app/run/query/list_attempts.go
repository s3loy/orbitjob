package query

import (
	"context"
	"time"
)

// AttemptItem is one row of the attempt trail: a single platform attempt, which
// is one Kubernetes Job. The Kubernetes identity is carried alongside the phase
// because a deleted-and-recreated Job keeps its name but not its UID, and the
// name alone is not proof of ownership.
type AttemptItem struct {
	ID                      int64      `json:"id"`
	RunID                   int64      `json:"run_id"`
	AttemptNumber           int        `json:"attempt_number"`
	Phase                   string     `json:"phase"`
	KubernetesJobName       string     `json:"kubernetes_job_name"`
	KubernetesJobUID        *string    `json:"kubernetes_job_uid"`
	ObservedResourceVersion *string    `json:"observed_resource_version"`
	StartedAt               *time.Time `json:"started_at"`
	CompletedAt             *time.Time `json:"completed_at"`
	CreatedAt               time.Time  `json:"created_at"`
}

type attemptLister interface {
	ListAttempts(ctx context.Context, in GetInput) ([]AttemptItem, error)
}

// ListAttemptsUseCase serves the attempt trail for one run.
type ListAttemptsUseCase struct {
	repo attemptLister
}

// NewListAttemptsUseCase builds a ListAttemptsUseCase backed by repo.
func NewListAttemptsUseCase(repo attemptLister) *ListAttemptsUseCase {
	return &ListAttemptsUseCase{repo: repo}
}

// List validates the run reference, then reads its attempts in order. A run
// with no attempts yields an empty slice, not an error.
func (uc *ListAttemptsUseCase) List(ctx context.Context, in GetInput) ([]AttemptItem, error) {
	normalized, err := NormalizeGetInput(in)
	if err != nil {
		return nil, err
	}
	return uc.repo.ListAttempts(ctx, normalized)
}

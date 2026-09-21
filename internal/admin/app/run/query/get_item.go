package query

import (
	"context"
	"time"
)

// GetItem is the read model for GET /api/v1/runs/:id. It carries the run's own
// ledger columns plus its full attempt trail, so one call answers "did it run,
// how many times, and who triggered it" without a second request.
type GetItem struct {
	ID            int64         `json:"id"`
	TenantID      string        `json:"tenant_id"`
	SourceUID     string        `json:"source_uid"`
	RevisionID    int64         `json:"revision_id"`
	OccurrenceKey string        `json:"occurrence_key"`
	Trigger       string        `json:"trigger"`
	Actor         string        `json:"actor"`
	Phase         string        `json:"phase"`
	Attempt       int           `json:"attempt"`
	MaxAttempts   int           `json:"max_attempts"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
	Attempts      []AttemptItem `json:"attempts"`
}

type runGetter interface {
	Get(ctx context.Context, in GetInput) (GetItem, error)
}

// GetRunUseCase serves one tenant-scoped run with its attempt trail.
type GetRunUseCase struct {
	repo runGetter
}

// NewGetRunUseCase builds a GetRunUseCase backed by repo.
func NewGetRunUseCase(repo runGetter) *GetRunUseCase {
	return &GetRunUseCase{repo: repo}
}

// Get validates the input, then reads the run and its attempts.
func (uc *GetRunUseCase) Get(ctx context.Context, in GetInput) (GetItem, error) {
	normalized, err := NormalizeGetInput(in)
	if err != nil {
		return GetItem{}, err
	}
	return uc.repo.Get(ctx, normalized)
}

package query

import (
	"context"
	"time"
)

// ListItem is the read model for one run in GET /api/v1/runs. It answers the
// ledger's question at list altitude: which run, in what phase, how many
// attempts it has taken, and who triggered it. The attempt trail itself is a
// detail concern and is not fetched here, so the list stays one round trip.
type ListItem struct {
	ID            int64     `json:"id"`
	TenantID      string    `json:"tenant_id"`
	SourceUID     string    `json:"source_uid"`
	RevisionID    int64     `json:"revision_id"`
	OccurrenceKey string    `json:"occurrence_key"`
	Trigger       string    `json:"trigger"`
	Actor         string    `json:"actor"`
	Phase         string    `json:"phase"`
	Attempt       int       `json:"attempt"`
	MaxAttempts   int       `json:"max_attempts"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type runLister interface {
	List(ctx context.Context, in ListInput) ([]ListItem, error)
}

// ListRunsUseCase serves the tenant-scoped run list.
type ListRunsUseCase struct {
	repo runLister
}

// NewListRunsUseCase builds a ListRunsUseCase backed by repo.
func NewListRunsUseCase(repo runLister) *ListRunsUseCase {
	return &ListRunsUseCase{repo: repo}
}

// List validates the input, then reads one page of runs.
func (uc *ListRunsUseCase) List(ctx context.Context, in ListInput) ([]ListItem, error) {
	normalized, err := NormalizeListInput(in)
	if err != nil {
		return nil, err
	}
	return uc.repo.List(ctx, normalized)
}

package query

import (
	"context"

	"orbitjob/internal/core/domain/sli"
)

// sliReader retrieves SLIs.
type sliReader interface {
	Get(ctx context.Context, tenantID string, id int64) (sli.Snapshot, error)
}

// GetSLIUseCase handles SLI retrieval.
type GetSLIUseCase struct {
	repo sliReader
}

// NewGetSLIUseCase creates a new use case.
func NewGetSLIUseCase(repo sliReader) *GetSLIUseCase {
	return &GetSLIUseCase{repo: repo}
}

// GetInput contains the parameters for retrieval.
type GetInput struct {
	ID int64
}

// GetItem is the read model for a single SLI.
type GetItem struct {
	ID                int64          `json:"id"`
	Name              string         `json:"name"`
	Description       *string        `json:"description,omitempty"`
	SLIType           string         `json:"sli_type"`
	SourceType        string         `json:"source_type"`
	SourceConfig      map[string]any `json:"source_config"`
	Aggregation       string         `json:"aggregation"`
	GoodEventCriteria map[string]any `json:"good_event_criteria"`
	Version           int            `json:"version"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

// Get retrieves a single SLI.
func (uc *GetSLIUseCase) Get(ctx context.Context, tenantID string, id int64) (GetItem, error) {
	snap, err := uc.repo.Get(ctx, tenantID, id)
	if err != nil {
		return GetItem{}, err
	}

	return GetItem{
		ID:                snap.ID,
		Name:              snap.Name,
		Description:       snap.Description,
		SLIType:           snap.SLIType,
		SourceType:        snap.SourceType,
		SourceConfig:      snap.SourceConfig,
		Aggregation:       snap.Aggregation,
		GoodEventCriteria: snap.GoodEventCriteria,
		Version:           snap.Version,
		CreatedAt:         snap.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:         snap.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}

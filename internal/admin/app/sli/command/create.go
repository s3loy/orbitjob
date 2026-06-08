package command

import (
	"context"

	"orbitjob/internal/core/domain/sli"
)

// sliWriter creates SLIs.
type sliWriter interface {
	Create(ctx context.Context, tenantID string, spec sli.CreateSpec) (sli.Snapshot, error)
}

// CreateSLIUseCase handles SLI creation.
type CreateSLIUseCase struct {
	repo sliWriter
}

// NewCreateSLIUseCase creates a new use case.
func NewCreateSLIUseCase(repo sliWriter) *CreateSLIUseCase {
	return &CreateSLIUseCase{repo: repo}
}

// CreateInput is the raw input for creating an SLI.
type CreateInput struct {
	TenantID          string
	Name              string
	Description       *string
	SLIType           string
	SourceType        string
	SourceConfig      map[string]any
	Aggregation       string
	GoodEventCriteria map[string]any
}

// CreateResult is the output of creating an SLI.
type CreateResult struct {
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
}

// Create validates and creates a new SLI.
func (uc *CreateSLIUseCase) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	spec, err := sli.NormalizeCreate(sli.CreateInput{
		Name:              in.Name,
		Description:       in.Description,
		SLIType:           in.SLIType,
		SourceType:        in.SourceType,
		SourceConfig:      in.SourceConfig,
		Aggregation:       in.Aggregation,
		GoodEventCriteria: in.GoodEventCriteria,
	})
	if err != nil {
		return CreateResult{}, err
	}

	snap, err := uc.repo.Create(ctx, in.TenantID, spec)
	if err != nil {
		return CreateResult{}, err
	}

	return CreateResult{
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
	}, nil
}

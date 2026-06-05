package command

import (
	"context"
	"time"

	"orbitjob/internal/core/domain/slo"
)

// sloWriter creates SLOs.
type sloWriter interface {
	Create(ctx context.Context, tenantID string, spec slo.CreateSpec) (slo.Snapshot, error)
}

// CreateSLOUseCase handles SLO creation.
type CreateSLOUseCase struct {
	repo sloWriter
}

// NewCreateSLOUseCase creates a new use case.
func NewCreateSLOUseCase(repo sloWriter) *CreateSLOUseCase {
	return &CreateSLOUseCase{repo: repo}
}

// CreateInput is the raw input for creating an SLO.
type CreateInput struct {
	TenantID          string
	Name              string
	Description       *string
	SLIID             int64
	Target            float64
	WindowType        string
	WindowDuration    time.Duration
	AlertFastBurnRate float64
	AlertSlowBurnRate float64
}

// CreateResult is the output of creating an SLO.
type CreateResult struct {
	ID                int64         `json:"id"`
	Name              string        `json:"name"`
	Description       *string       `json:"description,omitempty"`
	SLIID             int64         `json:"sli_id"`
	Target            float64       `json:"target"`
	WindowType        string        `json:"window_type"`
	WindowDuration    string        `json:"window_duration"`
	AlertFastBurnRate float64       `json:"alert_fast_burn_rate"`
	AlertSlowBurnRate float64       `json:"alert_slow_burn_rate"`
	Status            string        `json:"status"`
	Version           int           `json:"version"`
	CreatedAt         string        `json:"created_at"`
}

// Create validates and creates a new SLO.
func (uc *CreateSLOUseCase) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	spec, err := slo.NormalizeCreate(slo.CreateInput{
		Name:              in.Name,
		Description:       in.Description,
		SLIID:             in.SLIID,
		Target:            in.Target,
		WindowType:        in.WindowType,
		WindowDuration:    in.WindowDuration,
		AlertFastBurnRate: in.AlertFastBurnRate,
		AlertSlowBurnRate: in.AlertSlowBurnRate,
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
		SLIID:             snap.SLIID,
		Target:            snap.Target,
		WindowType:        snap.WindowType,
		WindowDuration:    snap.WindowDuration.String(),
		AlertFastBurnRate: snap.AlertFastBurnRate,
		AlertSlowBurnRate: snap.AlertSlowBurnRate,
		Status:            snap.Status,
		Version:           snap.Version,
		CreatedAt:         snap.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}

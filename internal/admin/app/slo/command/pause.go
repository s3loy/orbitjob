package command

import (
	"context"
	"fmt"

	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/validation"
)

// sloStatusChanger changes SLO status.
type sloStatusChanger interface {
	ChangeStatus(ctx context.Context, tenantID string, id int64, version int, status string) (slo.Snapshot, error)
}

// ChangeSLOStatusUseCase handles SLO pause/resume.
type ChangeSLOStatusUseCase struct {
	repo sloStatusChanger
}

// NewChangeSLOStatusUseCase creates a new use case.
func NewChangeSLOStatusUseCase(repo sloStatusChanger) *ChangeSLOStatusUseCase {
	return &ChangeSLOStatusUseCase{repo: repo}
}

// ChangeStatusInput contains the parameters for status change.
type ChangeStatusInput struct {
	ID      int64
	Version int
	Status  string
}

// ChangeStatusResult is the output of a status change.
type ChangeStatusResult struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

// ChangeStatus changes an SLO's status.
func (uc *ChangeSLOStatusUseCase) ChangeStatus(ctx context.Context, tenantID string, in ChangeStatusInput) (ChangeStatusResult, error) {
	if in.ID <= 0 {
		return ChangeStatusResult{}, validation.New("id", "id must be positive")
	}
	if !slo.ValidStatuses[in.Status] {
		return ChangeStatusResult{}, validation.New("status", fmt.Sprintf("invalid status: %s", in.Status))
	}

	snap, err := uc.repo.ChangeStatus(ctx, tenantID, in.ID, in.Version, in.Status)
	if err != nil {
		return ChangeStatusResult{}, err
	}

	return ChangeStatusResult{
		ID:     snap.ID,
		Status: snap.Status,
	}, nil
}

// Pause pauses an SLO.
func (uc *ChangeSLOStatusUseCase) Pause(ctx context.Context, tenantID string, in ChangeStatusInput) (ChangeStatusResult, error) {
	in.Status = slo.StatusPaused
	return uc.ChangeStatus(ctx, tenantID, in)
}

// Resume resumes an SLO.
func (uc *ChangeSLOStatusUseCase) Resume(ctx context.Context, tenantID string, in ChangeStatusInput) (ChangeStatusResult, error) {
	in.Status = slo.StatusActive
	return uc.ChangeStatus(ctx, tenantID, in)
}

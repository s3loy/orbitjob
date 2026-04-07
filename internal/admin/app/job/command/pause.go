package command

import (
	"context"
	"time"

	query "orbitjob/internal/admin/app/job/query"
	domainjob "orbitjob/internal/core/domain/job"
)

type jobStatusReader interface {
	Get(ctx context.Context, in query.GetInput) (query.GetItem, error)
}

type jobStatusChanger interface {
	ChangeStatus(ctx context.Context, in domainjob.ChangeStatusSpec, changedBy string) (domainjob.Snapshot, error)
}

// ChangeStatusInput is the admin command input for pause/resume lifecycle actions.
type ChangeStatusInput struct {
	ID        int64
	TenantID  string
	Version   int
	ChangedBy string
}

// ChangeStatusResult is the control-plane snapshot returned after pause/resume.
type ChangeStatusResult struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`
	Version  int    `json:"version"`

	NextRunAt *time.Time `json:"next_run_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type ChangeStatusUseCase struct {
	reader jobStatusReader
	repo   jobStatusChanger
}

func NewChangeStatusUseCase(reader jobStatusReader, repo jobStatusChanger) *ChangeStatusUseCase {
	return &ChangeStatusUseCase{
		reader: reader,
		repo:   repo,
	}
}

func (uc *ChangeStatusUseCase) Pause(ctx context.Context, in ChangeStatusInput) (ChangeStatusResult, error) {
	return uc.change(ctx, in, domainjob.ActionPause)
}

func (uc *ChangeStatusUseCase) Resume(ctx context.Context, in ChangeStatusInput) (ChangeStatusResult, error) {
	return uc.change(ctx, in, domainjob.ActionResume)
}

func (uc *ChangeStatusUseCase) change(
	ctx context.Context,
	in ChangeStatusInput,
	action string,
) (ChangeStatusResult, error) {
	getIn, err := query.NormalizeGetInput(query.GetInput{
		ID:       in.ID,
		TenantID: in.TenantID,
	})
	if err != nil {
		return ChangeStatusResult{}, err
	}

	current, err := uc.reader.Get(ctx, getIn)
	if err != nil {
		return ChangeStatusResult{}, err
	}

	nextStatus, err := nextStatusForAction(action, current.Status, in.Version)
	if err != nil {
		return ChangeStatusResult{}, err
	}

	out, err := uc.repo.ChangeStatus(ctx, domainjob.ChangeStatusSpec{
		ID:            in.ID,
		TenantID:      getIn.TenantID,
		Version:       in.Version,
		CurrentStatus: current.Status,
		NextStatus:    nextStatus,
		Action:        action,
	}, in.ChangedBy)
	if err != nil {
		return ChangeStatusResult{}, err
	}

	return ChangeStatusResult{
		ID:        out.ID,
		Name:      out.Name,
		TenantID:  out.TenantID,
		Status:    out.Status,
		Version:   out.Version,
		NextRunAt: out.NextRunAt,
		CreatedAt: out.CreatedAt,
		UpdatedAt: out.UpdatedAt,
	}, nil
}

func nextStatusForAction(action, currentStatus string, version int) (string, error) {
	switch action {
	case domainjob.ActionPause:
		return domainjob.Pause(currentStatus, version)
	case domainjob.ActionResume:
		return domainjob.Resume(currentStatus, version)
	default:
		return "", &domainjob.ValidationError{
			Field:   "action",
			Message: "must be one of: pause, resume",
		}
	}
}

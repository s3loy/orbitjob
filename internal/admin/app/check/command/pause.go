package command

import (
	"context"

	domaincheck "orbitjob/internal/core/domain/check"
)

type checkStatusChanger interface {
	ChangeStatus(ctx context.Context, tenantID string, id int64, version int, action string) (domaincheck.Snapshot, error)
}

type ChangeStatusInput struct {
	TenantID string
	ID       int64
	Version  int
}

type ChangeStatusResult struct {
	ID      int64  `json:"id"`
	Status  string `json:"status"`
	Version int    `json:"version"`
}

type PauseCheckUseCase struct {
	repo checkStatusChanger
}

func NewPauseCheckUseCase(repo checkStatusChanger) *PauseCheckUseCase {
	return &PauseCheckUseCase{repo: repo}
}

func (uc *PauseCheckUseCase) Pause(ctx context.Context, in ChangeStatusInput) (ChangeStatusResult, error) {
	out, err := uc.repo.ChangeStatus(ctx, in.TenantID, in.ID, in.Version, domaincheck.ActionPause)
	if err != nil {
		return ChangeStatusResult{}, err
	}
	return ChangeStatusResult{ID: out.ID, Status: out.Status, Version: out.Version}, nil
}

type ResumeCheckUseCase struct {
	repo checkStatusChanger
}

func NewResumeCheckUseCase(repo checkStatusChanger) *ResumeCheckUseCase {
	return &ResumeCheckUseCase{repo: repo}
}

func (uc *ResumeCheckUseCase) Resume(ctx context.Context, in ChangeStatusInput) (ChangeStatusResult, error) {
	out, err := uc.repo.ChangeStatus(ctx, in.TenantID, in.ID, in.Version, domaincheck.ActionResume)
	if err != nil {
		return ChangeStatusResult{}, err
	}
	return ChangeStatusResult{ID: out.ID, Status: out.Status, Version: out.Version}, nil
}

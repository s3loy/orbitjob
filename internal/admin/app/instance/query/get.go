package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	domaininstance "orbitjob/internal/core/domain/instance"
	"orbitjob/internal/domain/resource"
)

type instanceGetter interface {
	GetByRunID(ctx context.Context, runID string) (domaininstance.Snapshot, error)
}

type GetInstanceUseCase struct {
	repo instanceGetter
}

func NewGetInstanceUseCase(repo instanceGetter) *GetInstanceUseCase {
	return &GetInstanceUseCase{repo: repo}
}

func (uc *GetInstanceUseCase) Get(ctx context.Context, runID string) (*InstanceItem, error) {
	s, err := uc.repo.GetByRunID(ctx, runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &resource.NotFoundError{Resource: "instance", ID: runID}
		}
		return nil, fmt.Errorf("get instance: %w", err)
	}
	return &InstanceItem{
		RunID:             s.RunID,
		TenantID:          s.TenantID,
		JobID:             s.JobID,
		TriggerSource:     s.TriggerSource,
		Status:            s.Status,
		Priority:          s.Priority,
		EffectivePriority: s.EffectivePriority,
		Attempt:           s.Attempt,
		MaxAttempt:        s.MaxAttempt,
		ScheduledAt:       s.ScheduledAt,
		StartedAt:         s.StartedAt,
		FinishedAt:        s.FinishedAt,
		WorkerID:          s.WorkerID,
		ResultCode:        s.ResultCode,
		ErrorMsg:          s.ErrorMsg,
		RetryAt:           s.RetryAt,
		DispatchedAt:      s.DispatchedAt,
		CreatedAt:         s.CreatedAt,
		UpdatedAt:         s.UpdatedAt,
		Version:           s.Version,
	}, nil
}

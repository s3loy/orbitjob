package query

import (
	"context"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
)

type instanceLister interface {
	List(ctx context.Context, tenantID, status string, limit, offset int) ([]domaininstance.Snapshot, error)
}

type ListInstancesUseCase struct {
	repo instanceLister
}

func NewListInstancesUseCase(repo instanceLister) *ListInstancesUseCase {
	return &ListInstancesUseCase{repo: repo}
}

type ListInstancesInput struct {
	TenantID string
	Status   string
	Limit    int
	Offset   int
}

type InstanceItem struct {
	RunID              string     `json:"run_id"`
	TenantID           string     `json:"tenant_id"`
	JobID              int64      `json:"job_id"`
	TriggerSource      string     `json:"trigger_source"`
	Status             string     `json:"status"`
	Priority           int        `json:"priority"`
	EffectivePriority  int        `json:"effective_priority"`
	Attempt            int        `json:"attempt"`
	MaxAttempt         int        `json:"max_attempt"`
	ScheduledAt        time.Time  `json:"scheduled_at"`
	StartedAt          *time.Time `json:"started_at"`
	FinishedAt         *time.Time `json:"finished_at"`
	WorkerID           *string    `json:"worker_id"`
	ResultCode         *string    `json:"result_code"`
	ErrorMsg           *string    `json:"error_msg"`
	RetryAt            *time.Time `json:"retry_at"`
	DispatchedAt       *time.Time `json:"dispatched_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	Version            int        `json:"version"`
}

func (uc *ListInstancesUseCase) List(ctx context.Context, in ListInstancesInput) ([]InstanceItem, error) {
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if in.Offset < 0 {
		in.Offset = 0
	}

	snapshots, err := uc.repo.List(ctx, in.TenantID, in.Status, limit, in.Offset)
	if err != nil {
		return nil, err
	}

	out := make([]InstanceItem, len(snapshots))
	for i, s := range snapshots {
		out[i] = InstanceItem{
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
		}
	}
	return out, nil
}

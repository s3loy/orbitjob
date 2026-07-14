package query

import (
	"context"
	"time"

	domaincheck "orbitjob/internal/core/domain/check"
)

type checkGetter interface {
	Get(ctx context.Context, tenantID string, id int64) (domaincheck.Snapshot, error)
}

// GetResult is the result of getting a single check.
type GetResult struct {
	ID             int64                       `json:"id"`
	Name           string                      `json:"name"`
	Description    *string                     `json:"description,omitempty"`
	TenantID       string                      `json:"tenant_id"`
	Status         string                      `json:"status"`
	CheckType      string                      `json:"check_type"`
	CheckConfig    map[string]any              `json:"check_config"`
	AssertionRules []domaincheck.AssertionRule `json:"assertion_rules"`
	ScheduleType   string                      `json:"schedule_type"`
	CronExpr       *string                     `json:"cron_expr,omitempty"`
	IntervalSec    *int                        `json:"interval_sec,omitempty"`
	Timezone       string                      `json:"timezone"`
	TimeoutSec     int                         `json:"timeout_sec"`
	RetryLimit     int                         `json:"retry_limit"`
	Priority       int                         `json:"priority"`
	Labels         map[string]any              `json:"labels"`
	NextRunAt      *time.Time                  `json:"next_run_at,omitempty"`
	Version        int                         `json:"version"`
	CreatedAt      time.Time                   `json:"created_at"`
	UpdatedAt      time.Time                   `json:"updated_at"`
}

type GetCheckUseCase struct {
	repo checkGetter
}

func NewGetCheckUseCase(repo checkGetter) *GetCheckUseCase {
	return &GetCheckUseCase{repo: repo}
}

func (uc *GetCheckUseCase) Get(ctx context.Context, tenantID string, id int64) (GetResult, error) {
	out, err := uc.repo.Get(ctx, tenantID, id)
	if err != nil {
		return GetResult{}, err
	}
	return GetResult{
		ID:             out.ID,
		Name:           out.Name,
		Description:    out.Description,
		TenantID:       out.TenantID,
		Status:         out.Status,
		CheckType:      out.CheckType,
		CheckConfig:    out.CheckConfig,
		AssertionRules: out.AssertionRules,
		ScheduleType:   out.ScheduleType,
		CronExpr:       out.CronExpr,
		IntervalSec:    out.IntervalSec,
		Timezone:       out.Timezone,
		TimeoutSec:     out.TimeoutSec,
		RetryLimit:     out.RetryLimit,
		Priority:       out.Priority,
		Labels:         out.Labels,
		NextRunAt:      out.NextRunAt,
		Version:        out.Version,
		CreatedAt:      out.CreatedAt,
		UpdatedAt:      out.UpdatedAt,
	}, nil
}

package command

import (
	"context"
	"time"

	domaincheck "orbitjob/internal/core/domain/check"
)

type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

type checkCreator interface {
	Create(ctx context.Context, spec domaincheck.CreateSpec) (domaincheck.Snapshot, error)
}

// CreateInput is the application-layer input for creating a check.
type CreateInput struct {
	Name           string
	Description    *string
	TenantID       string
	CheckType      string
	CheckConfig    map[string]any
	AssertionRules []domaincheck.AssertionRule
	ScheduleType   string
	CronExpr       *string
	IntervalSec    *int
	Timezone       string
	TimeoutSec     int
	RetryLimit     int
	Priority       int
	Labels         map[string]any
}

// CreateResult is the application-layer result for creating a check.
type CreateResult struct {
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

type CreateCheckUseCase struct {
	repo  checkCreator
	clock clock
}

func NewCreateCheckUseCase(repo checkCreator) *CreateCheckUseCase {
	return &CreateCheckUseCase{repo: repo, clock: realClock{}}
}

func (uc *CreateCheckUseCase) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	spec, err := domaincheck.NormalizeCreate(uc.clock.Now(), domaincheck.CreateInput{
		Name:           in.Name,
		Description:    in.Description,
		TenantID:       in.TenantID,
		CheckType:      in.CheckType,
		CheckConfig:    in.CheckConfig,
		AssertionRules: in.AssertionRules,
		ScheduleType:   in.ScheduleType,
		CronExpr:       in.CronExpr,
		IntervalSec:    in.IntervalSec,
		Timezone:       in.Timezone,
		TimeoutSec:     in.TimeoutSec,
		RetryLimit:     in.RetryLimit,
		Priority:       in.Priority,
		Labels:         in.Labels,
	})
	if err != nil {
		return CreateResult{}, err
	}

	out, err := uc.repo.Create(ctx, spec)
	if err != nil {
		return CreateResult{}, err
	}

	return CreateResult{
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

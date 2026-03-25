package http

import (
	query "orbitjob/internal/admin/app/job/query"
	domainjob "orbitjob/internal/domain/job"
)

// CreateJobRequest defines the HTTP payload for creating a job.
// It is a transport-layer input and should be converted into domainjob.CreateInput.
type CreateJobRequest struct {
	Name        string  `json:"name" binding:"required,max=128"`
	TenantID    string  `json:"tenant_id"`
	TriggerType string  `json:"trigger_type" binding:"required,oneof=cron manual"`
	CronExpr    *string `json:"cron_expr"`
	Timezone    string  `json:"timezone"`

	HandlerType    string         `json:"handler_type" binding:"required,max=32"`
	HandlerPayload map[string]any `json:"handler_payload"`

	TimeoutSec           int    `json:"timeout_sec" binding:"omitempty,min=1"`
	RetryLimit           int    `json:"retry_limit" binding:"omitempty,min=0"`
	RetryBackoffSec      int    `json:"retry_backoff_sec" binding:"omitempty,min=0"`
	RetryBackoffStrategy string `json:"retry_backoff_strategy" binding:"omitempty,oneof=fixed exponential"`
	ConcurrencyPolicy    string `json:"concurrency_policy" binding:"omitempty,oneof=allow forbid replace"`
	MisfirePolicy        string `json:"misfire_policy" binding:"omitempty,oneof=skip fire_now catch_up"`
}

// ToCreateInput converts the HTTP request into a domain input.
// Validation, defaulting and next_run_at calculation are handled in the domain layer.
func (r CreateJobRequest) ToCreateInput() domainjob.CreateInput {
	return domainjob.CreateInput{
		Name:                 r.Name,
		TenantID:             r.TenantID,
		TriggerType:          r.TriggerType,
		CronExpr:             r.CronExpr,
		Timezone:             r.Timezone,
		HandlerType:          r.HandlerType,
		HandlerPayload:       r.HandlerPayload,
		TimeoutSec:           r.TimeoutSec,
		RetryLimit:           r.RetryLimit,
		RetryBackoffSec:      r.RetryBackoffSec,
		RetryBackoffStrategy: r.RetryBackoffStrategy,
		ConcurrencyPolicy:    r.ConcurrencyPolicy,
		MisfirePolicy:        r.MisfirePolicy,
	}
}

// ListJobsRequest defines the query parameters for listing jobs.
type ListJobsRequest struct {
	TenantID string `form:"tenant_id" binding:"omitempty,max=64"`
	Status   string `form:"status" binding:"omitempty,oneof=active paused"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Offset   int    `form:"offset" binding:"omitempty,min=0"`
}

// ToListInput converts the HTTP query parameters into a control-plane query input.
func (r ListJobsRequest) ToListInput() query.ListInput {
	return query.ListInput{
		TenantID: r.TenantID,
		Status:   r.Status,
		Limit:    r.Limit,
		Offset:   r.Offset,
	}
}

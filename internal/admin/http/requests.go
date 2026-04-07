package http

import (
	command "orbitjob/internal/admin/app/job/command"
	query "orbitjob/internal/admin/app/job/query"
)

// CreateJobRequest defines the HTTP payload for creating a job.
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

// ToCreateInput converts the HTTP request into an admin command input.
func (r CreateJobRequest) ToCreateInput() command.CreateInput {
	return command.CreateInput{
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

// GetJobRequest defines the route and query parameters for reading one job.
type GetJobRequest struct {
	ID       int64  `uri:"id" binding:"required,min=1"`
	TenantID string `form:"tenant_id" binding:"omitempty,max=64"`
}

// ToGetInput converts the HTTP route and query parameters into a control-plane query input.
func (r GetJobRequest) ToGetInput() query.GetInput {
	return query.GetInput{
		ID:       r.ID,
		TenantID: r.TenantID,
	}
}

// UpdateJobRequest defines the route, query, and payload fields for updating one job.
type UpdateJobRequest struct {
	ID       int64
	TenantID string
	Version  int `json:"version" binding:"required,min=1"`

	Name        string  `json:"name" binding:"required,max=128"`
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

// ToUpdateInput converts the HTTP request into an admin command input.
func (r UpdateJobRequest) ToUpdateInput(changedBy string) command.UpdateInput {
	return command.UpdateInput{
		ID:                   r.ID,
		TenantID:             r.TenantID,
		ChangedBy:            changedBy,
		Version:              r.Version,
		Name:                 r.Name,
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

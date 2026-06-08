package http

import (
	"fmt"
	"time"

	checkcommand "orbitjob/internal/admin/app/check/command"
	checkquery "orbitjob/internal/admin/app/check/query"
	checkrunquery "orbitjob/internal/admin/app/checkrun/query"
	command "orbitjob/internal/admin/app/job/command"
	query "orbitjob/internal/admin/app/job/query"
	slicommand "orbitjob/internal/admin/app/sli/command"
	sliquery "orbitjob/internal/admin/app/sli/query"
	slocommand "orbitjob/internal/admin/app/slo/command"
	sloquery "orbitjob/internal/admin/app/slo/query"
	slobudgetquery "orbitjob/internal/admin/app/slobudget/query"
	sloalertquery "orbitjob/internal/admin/app/sloalert/query"
	domaincheck "orbitjob/internal/core/domain/check"
	domainjob "orbitjob/internal/core/domain/job"
)

// CreateJobRequest defines the HTTP payload for creating a job.
type CreateJobRequest struct {
	Name         string  `json:"name" binding:"required,max=128"`
	TenantID     string  `json:"tenant_id" binding:"omitempty,max=64"`
	Priority     int     `json:"priority" binding:"omitempty,min=0"`
	PartitionKey *string `json:"partition_key" binding:"omitempty,max=64"`
	TriggerType  string  `json:"trigger_type" binding:"required,oneof=cron manual"`
	CronExpr     *string `json:"cron_expr"`
	Timezone     string  `json:"timezone" binding:"omitempty,max=64"`

	HandlerType    string         `json:"handler_type" binding:"required,oneof=exec http,max=32"`
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
		Priority:             r.Priority,
		PartitionKey:         r.PartitionKey,
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

type jobIDURI struct {
	ID int64 `uri:"id" binding:"required,min=1"`
}

type tenantQueryRequest struct {
	TenantID string `form:"tenant_id" binding:"omitempty,max=64"`
}

type actorIDHeaderRequest struct {
	ActorID string `header:"X-Actor-ID" binding:"required,max=128"`
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

	Name         *string `json:"name" binding:"omitempty,max=128"`
	Priority     *int    `json:"priority" binding:"omitempty,min=0"`
	PartitionKey *string `json:"partition_key" binding:"omitempty,max=64"`
	TriggerType  *string `json:"trigger_type" binding:"omitempty,oneof=cron manual"`
	CronExpr     *string `json:"cron_expr"`
	Timezone     *string `json:"timezone" binding:"omitempty,max=64"`

	HandlerType    *string        `json:"handler_type" binding:"omitempty,oneof=exec http,max=32"`
	HandlerPayload map[string]any `json:"handler_payload"`

	TimeoutSec           *int    `json:"timeout_sec" binding:"omitempty,min=1"`
	RetryLimit           *int    `json:"retry_limit" binding:"omitempty,min=0"`
	RetryBackoffSec      *int    `json:"retry_backoff_sec" binding:"omitempty,min=0"`
	RetryBackoffStrategy *string `json:"retry_backoff_strategy" binding:"omitempty,oneof=fixed exponential"`
	ConcurrencyPolicy    *string `json:"concurrency_policy" binding:"omitempty,oneof=allow forbid replace"`
	MisfirePolicy        *string `json:"misfire_policy" binding:"omitempty,oneof=skip fire_now catch_up"`
}

// ChangeStatusRequest defines the route, query, and payload fields for pause/resume.
type ChangeStatusRequest struct {
	ID       int64
	TenantID string
	Version  int `json:"version" binding:"required,min=1"`
}

// ToChangeStatusInput converts the HTTP request into a lifecycle status command input.
func (r ChangeStatusRequest) ToChangeStatusInput(changedBy string) command.ChangeStatusInput {
	return command.ChangeStatusInput{
		ID:        r.ID,
		TenantID:  r.TenantID,
		Version:   r.Version,
		ChangedBy: changedBy,
	}
}

// ToUpdateInput merges sparse HTTP update fields onto the current job state.
func (r UpdateJobRequest) ToUpdateInput(current query.GetItem, changedBy string) command.UpdateInput {
	triggerType := current.TriggerType
	if r.TriggerType != nil {
		triggerType = *r.TriggerType
	}

	partitionKey := cloneOptionalString(current.PartitionKey)
	if r.PartitionKey != nil {
		partitionKey = cloneOptionalString(r.PartitionKey)
	}

	cronExpr := cloneOptionalString(current.CronExpr)
	if r.CronExpr != nil {
		cronExpr = cloneOptionalString(r.CronExpr)
	}
	if triggerType == domainjob.TriggerTypeManual && r.TriggerType != nil && *r.TriggerType == domainjob.TriggerTypeManual && r.CronExpr == nil {
		cronExpr = nil
	}

	return command.UpdateInput{
		ID:                   r.ID,
		TenantID:             r.TenantID,
		ChangedBy:            changedBy,
		Version:              r.Version,
		Name:                 stringValueOrDefault(r.Name, current.Name),
		Priority:             intValueOrDefault(r.Priority, current.Priority),
		PartitionKey:         partitionKey,
		TriggerType:          triggerType,
		CronExpr:             cronExpr,
		Timezone:             stringValueOrDefault(r.Timezone, current.Timezone),
		HandlerType:          stringValueOrDefault(r.HandlerType, current.HandlerType),
		HandlerPayload:       mapValueOrDefault(r.HandlerPayload, current.HandlerPayload),
		TimeoutSec:           intValueOrDefault(r.TimeoutSec, current.TimeoutSec),
		RetryLimit:           intValueOrDefault(r.RetryLimit, current.RetryLimit),
		RetryBackoffSec:      intValueOrDefault(r.RetryBackoffSec, current.RetryBackoffSec),
		RetryBackoffStrategy: stringValueOrDefault(r.RetryBackoffStrategy, current.RetryBackoffStrategy),
		ConcurrencyPolicy:    stringValueOrDefault(r.ConcurrencyPolicy, current.ConcurrencyPolicy),
		MisfirePolicy:        stringValueOrDefault(r.MisfirePolicy, current.MisfirePolicy),
	}
}

func stringValueOrDefault(value *string, fallback string) string {
	if value == nil {
		return fallback
	}

	return *value
}

func intValueOrDefault(value *int, fallback int) int {
	if value == nil {
		return fallback
	}

	return *value
}

func mapValueOrDefault(value, fallback map[string]any) map[string]any {
	if value == nil {
		return cloneMap(fallback)
	}

	return cloneMap(value)
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}

	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}

	return out
}

func cloneOptionalString(in *string) *string {
	if in == nil {
		return nil
	}

	value := *in
	return &value
}

type instanceRunIDURI struct {
	RunID string `uri:"run_id" binding:"required,min=1,max=64"`
}

// CancelInstanceRequest defines the HTTP payload for canceling an instance.
type CancelInstanceRequest struct {
	Version int `json:"version" binding:"required,min=1"`
}

type ListInstancesRequest struct {
	Status string `form:"status" binding:"omitempty,oneof=pending dispatched running retry_wait success failed canceled"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Offset int    `form:"offset" binding:"omitempty,min=0"`
}

// ==================== Check Requests ====================

// CreateCheckRequest defines the HTTP payload for creating a check.
type CreateCheckRequest struct {
	Name           string                  `json:"name" binding:"required,max=128"`
	Description    *string                 `json:"description" binding:"omitempty,max=512"`
	TenantID       string                  `json:"tenant_id" binding:"omitempty,max=64"`
	CheckType      string                  `json:"check_type" binding:"required,oneof=http_health,max=32"`
	CheckConfig    map[string]any          `json:"check_config"`
	AssertionRules []domaincheck.AssertionRule `json:"assertion_rules"`
	ScheduleType   string                  `json:"schedule_type" binding:"omitempty,oneof=cron interval"`
	CronExpr       *string                 `json:"cron_expr"`
	IntervalSec    *int                    `json:"interval_sec" binding:"omitempty,min=1"`
	Timezone       string                  `json:"timezone" binding:"omitempty,max=64"`
	TimeoutSec     int                     `json:"timeout_sec" binding:"omitempty,min=1"`
	RetryLimit     int                     `json:"retry_limit" binding:"omitempty,min=0"`
	Priority       int                     `json:"priority" binding:"omitempty,min=0"`
	Labels         map[string]any          `json:"labels"`
}

func (r CreateCheckRequest) ToCreateInput() checkcommand.CreateInput {
	return checkcommand.CreateInput{
		Name:           r.Name,
		Description:    r.Description,
		TenantID:       r.TenantID,
		CheckType:      r.CheckType,
		CheckConfig:    r.CheckConfig,
		AssertionRules: r.AssertionRules,
		ScheduleType:   r.ScheduleType,
		CronExpr:       r.CronExpr,
		IntervalSec:    r.IntervalSec,
		Timezone:       r.Timezone,
		TimeoutSec:     r.TimeoutSec,
		RetryLimit:     r.RetryLimit,
		Priority:       r.Priority,
		Labels:         r.Labels,
	}
}

// ListChecksRequest defines the query parameters for listing checks.
type ListChecksRequest struct {
	TenantID string `form:"tenant_id" binding:"omitempty,max=64"`
	Status   string `form:"status" binding:"omitempty,oneof=active paused"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Offset   int    `form:"offset" binding:"omitempty,min=0"`
}

func (r ListChecksRequest) ToListInput() checkquery.ListChecksInput {
	var status *string
	if r.Status != "" {
		status = &r.Status
	}
	return checkquery.ListChecksInput{
		TenantID: r.TenantID,
		Status:   status,
		Limit:    r.Limit,
		Offset:   r.Offset,
	}
}

// GetCheckRequest defines the route and query parameters for reading one check.
type GetCheckRequest struct {
	ID       int64  `uri:"id" binding:"required,min=1"`
	TenantID string `form:"tenant_id" binding:"omitempty,max=64"`
}

type checkIDURI struct {
	ID int64 `uri:"id" binding:"required,min=1"`
}

// ChangeCheckStatusRequest defines the payload for pause/resume checks.
type ChangeCheckStatusRequest struct {
	ID       int64
	TenantID string
	Version  int `json:"version" binding:"required,min=1"`
}

func (r ChangeCheckStatusRequest) ToChangeStatusInput() checkcommand.ChangeStatusInput {
	return checkcommand.ChangeStatusInput{
		ID:       r.ID,
		TenantID: r.TenantID,
		Version:  r.Version,
	}
}

// ListCheckRunsRequest defines the query parameters for listing check runs.
type ListCheckRunsRequest struct {
	TenantID string `form:"tenant_id" binding:"omitempty,max=64"`
	CheckID  int64  `form:"check_id" binding:"omitempty,min=1"`
	Status   string `form:"status" binding:"omitempty,oneof=pending running success failed"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Offset   int    `form:"offset" binding:"omitempty,min=0"`
}

func (r ListCheckRunsRequest) ToListInput() checkrunquery.ListCheckRunsInput {
	var status *string
	if r.Status != "" {
		status = &r.Status
	}
	var checkID *int64
	if r.CheckID > 0 {
		checkID = &r.CheckID
	}
	return checkrunquery.ListCheckRunsInput{
		TenantID: r.TenantID,
		CheckID:  checkID,
		Status:   status,
		Limit:    r.Limit,
		Offset:   r.Offset,
	}
}

// GetCheckRunRequest defines the route parameters for reading one check run.
type GetCheckRunRequest struct {
	ID int64 `uri:"id" binding:"required,min=1"`
}

// --- SLI Requests ---

type CreateSLIRequest struct {
	Name              string         `json:"name" binding:"required,max=128"`
	Description       *string        `json:"description,omitempty"`
	SLIType           string         `json:"sli_type" binding:"required,oneof=availability latency quality custom"`
	SourceType        string         `json:"source_type" binding:"omitempty,oneof=check_run"`
	SourceConfig      map[string]any `json:"source_config"`
	Aggregation       string         `json:"aggregation" binding:"omitempty,oneof=ratio count"`
	GoodEventCriteria map[string]any `json:"good_event_criteria"`
}

func (r CreateSLIRequest) ToCreateInput() slicommand.CreateInput {
	return slicommand.CreateInput{
		Name:              r.Name,
		Description:       r.Description,
		SLIType:           r.SLIType,
		SourceType:        r.SourceType,
		SourceConfig:      r.SourceConfig,
		Aggregation:       r.Aggregation,
		GoodEventCriteria: r.GoodEventCriteria,
	}
}

type sliIDURI struct {
	ID int64 `uri:"id" binding:"required,min=1"`
}

type GetSLIRequest struct {
	ID int64 `uri:"id" binding:"required,min=1"`
}

type ListSLIsRequest struct {
	TenantID string `form:"tenant_id"`
	Limit    int    `form:"limit,default=50" binding:"min=1,max=100"`
	Offset   int    `form:"offset,default=0" binding:"min=0"`
}

func (r ListSLIsRequest) ToListInput() sliquery.ListInput {
	return sliquery.ListInput{
		Limit:  r.Limit,
		Offset: r.Offset,
	}
}

// --- SLO Requests ---

// CreateSLORequest defines the HTTP payload for creating an SLO.
type CreateSLORequest struct {
	Name              string  `json:"name" binding:"required,max=128"`
	Description       *string `json:"description,omitempty"`
	SLIID             int64   `json:"sli_id" binding:"required,min=1"`
	Target            float64 `json:"target" binding:"required,min=0.0001,max=1"`
	WindowType        string  `json:"window_type" binding:"omitempty,oneof=rolling calendar"`
	WindowDuration    string  `json:"window_duration" binding:"required"`
	AlertFastBurnRate float64 `json:"alert_fast_burn_rate" binding:"omitempty,min=0.1"`
	AlertSlowBurnRate float64 `json:"alert_slow_burn_rate" binding:"omitempty,min=0.1"`
}

// ToCreateInput converts the HTTP request into a domain command input.
func (r CreateSLORequest) ToCreateInput() (slocommand.CreateInput, error) {
	dur, err := time.ParseDuration(r.WindowDuration)
	if err != nil {
		return slocommand.CreateInput{}, fmt.Errorf("invalid window_duration: %w", err)
	}
	return slocommand.CreateInput{
		Name:              r.Name,
		Description:       r.Description,
		SLIID:             r.SLIID,
		Target:            r.Target,
		WindowType:        r.WindowType,
		WindowDuration:    dur,
		AlertFastBurnRate: r.AlertFastBurnRate,
		AlertSlowBurnRate: r.AlertSlowBurnRate,
	}, nil
}

type sloIDURI struct {
	ID int64 `uri:"id" binding:"required,min=1"`
}

type GetSLORequest struct {
	ID int64 `uri:"id" binding:"required,min=1"`
}

type ListSLOsRequest struct {
	TenantID string `form:"tenant_id"`
	Limit    int    `form:"limit,default=50" binding:"min=1,max=100"`
	Offset   int    `form:"offset,default=0" binding:"min=0"`
}

func (r ListSLOsRequest) ToListInput() sloquery.ListInput {
	return sloquery.ListInput{
		Limit:  r.Limit,
		Offset: r.Offset,
	}
}

type ChangeSLOStatusRequest struct {
	Version int `json:"version" binding:"required,min=1"`
}

func (r ChangeSLOStatusRequest) ToChangeStatusInput(id int64) slocommand.ChangeStatusInput {
	return slocommand.ChangeStatusInput{
		ID:      id,
		Version: r.Version,
	}
}

type GetSLOBudgetRequest struct {
	SLOID int64 `uri:"id" binding:"required,min=1"`
}

type ListSLOBudgetsRequest struct {
	TenantID string `form:"tenant_id"`
	Limit    int    `form:"limit,default=50" binding:"min=1,max=100"`
	Offset   int    `form:"offset,default=0" binding:"min=0"`
}

func (r ListSLOBudgetsRequest) ToListInput() slobudgetquery.ListInput {
	return slobudgetquery.ListInput{
		SLOID:  0, // will be set from URL
		Limit:  r.Limit,
		Offset: r.Offset,
	}
}

type GetSLOAlertRequest struct {
	ID int64 `uri:"id" binding:"required,min=1"`
}

type ListSLOAlertsRequest struct {
	TenantID string `form:"tenant_id"`
	SLOID    *int64 `form:"slo_id,omitempty"`
	Status   *string `form:"status,omitempty"`
	Limit    int    `form:"limit,default=50" binding:"min=1,max=100"`
	Offset   int    `form:"offset,default=0" binding:"min=0"`
}

func (r ListSLOAlertsRequest) ToListInput() sloalertquery.ListInput {
	return sloalertquery.ListInput{
		SLOID:  r.SLOID,
		Status: r.Status,
		Limit:  r.Limit,
		Offset: r.Offset,
	}
}

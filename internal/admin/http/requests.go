package http

import (
	"encoding/json"
	"fmt"
	"time"

	apikeycommand "orbitjob/internal/admin/app/apikey/command"
	checkcommand "orbitjob/internal/admin/app/check/command"
	checkquery "orbitjob/internal/admin/app/check/query"
	checkrunquery "orbitjob/internal/admin/app/checkrun/query"
	jobquery "orbitjob/internal/admin/app/job/query"
	policycommand "orbitjob/internal/admin/app/policy/command"
	resourcegroupcommand "orbitjob/internal/admin/app/resourcegroup/command"
	slicommand "orbitjob/internal/admin/app/sli/command"
	sliquery "orbitjob/internal/admin/app/sli/query"
	slocommand "orbitjob/internal/admin/app/slo/command"
	sloquery "orbitjob/internal/admin/app/slo/query"
	sloalertquery "orbitjob/internal/admin/app/sloalert/query"
	slobudgetquery "orbitjob/internal/admin/app/slobudget/query"
	tenantcommand "orbitjob/internal/admin/app/tenant/command"
	tenantquery "orbitjob/internal/admin/app/tenant/query"
	domaincheck "orbitjob/internal/core/domain/check"
)

// ListJobsRequest defines the query parameters for listing job definitions.
//
// There is no status filter: a definition is an immutable revision, so it has
// no mutable status to filter on.
type ListJobsRequest struct {
	TenantID string `form:"tenant_id" binding:"omitempty,len=26"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Offset   int    `form:"offset" binding:"omitempty,min=0"`
}

// ToListInput converts the HTTP query parameters into a control-plane query input.
func (r ListJobsRequest) ToListInput() jobquery.ListInput {
	return jobquery.ListInput{
		TenantID: r.TenantID,
		Limit:    r.Limit,
		Offset:   r.Offset,
	}
}

// GetJobRequest defines the route and query parameters for reading one job. The
// id is the active revision id, which is the identity the read model exposes.
type GetJobRequest struct {
	ID       int64  `uri:"id" binding:"required,min=1"`
	TenantID string `form:"tenant_id" binding:"omitempty,len=26"`
}

type jobIDURI struct {
	ID int64 `uri:"id" binding:"required,min=1"`
}

type tenantQueryRequest struct {
	TenantID string `form:"tenant_id" binding:"omitempty,len=26"`
}

// ToGetInput converts the HTTP route and query parameters into a control-plane query input.
func (r GetJobRequest) ToGetInput() jobquery.GetInput {
	return jobquery.GetInput{
		ID:       r.ID,
		TenantID: r.TenantID,
	}
}

type instanceRunIDURI struct {
	RunID int64 `uri:"run_id" binding:"required,min=1"`
}

// ListInstancesRequest filters the run ledger. phase is the ledger's own
// vocabulary: a run is Pending, Running or Succeeded, and renaming that to a
// client-side synonym would make the API disagree with the table it reads.
type ListInstancesRequest struct {
	Phase  string `form:"phase" binding:"omitempty,oneof=Pending CreatingAttempt Running RetryWaiting Succeeded Failed CancelRequested Canceled CancelUnknown"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Offset int    `form:"offset" binding:"omitempty,min=0"`
}

// VersionRequest is the optimistic-locking body shared by the delete routes
// whose stores still check a version. It exists as a named type so the OpenAPI
// document declares the body those handlers already parse.
type VersionRequest struct {
	Version int `json:"version" binding:"required,min=1"`
}

// ==================== Check Requests ====================

// CreateCheckRequest defines the HTTP payload for creating a check.
type CreateCheckRequest struct {
	Name           string                      `json:"name" binding:"required,max=128"`
	Description    *string                     `json:"description" binding:"omitempty,max=512"`
	TenantID       string                      `json:"tenant_id" binding:"omitempty,len=26"`
	CheckType      string                      `json:"check_type" binding:"required,oneof=http_health,max=32"`
	CheckConfig    map[string]any              `json:"check_config"`
	AssertionRules []domaincheck.AssertionRule `json:"assertion_rules"`
	ScheduleType   string                      `json:"schedule_type" binding:"omitempty,oneof=cron interval"`
	CronExpr       *string                     `json:"cron_expr"`
	// The interval floor matches the domain's MinimumIntervalSec: every
	// occurrence is its own Kubernetes Job, so probes this fast would spend
	// the cluster on work nobody reads.
	IntervalSec *int           `json:"interval_sec" binding:"omitempty,min=30"`
	Timezone    string         `json:"timezone" binding:"omitempty,max=64"`
	TimeoutSec  int            `json:"timeout_sec" binding:"omitempty,min=1"`
	RetryLimit  int            `json:"retry_limit" binding:"omitempty,min=0"`
	Priority    int            `json:"priority" binding:"omitempty,min=0"`
	Labels      map[string]any `json:"labels"`
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
	TenantID string `form:"tenant_id" binding:"omitempty,len=26"`
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
	TenantID string `form:"tenant_id" binding:"omitempty,len=26"`
}

type checkIDURI struct {
	ID int64 `uri:"id" binding:"required,min=1"`
}

// ChangeCheckStatusRequest defines the payload for pause/resume checks.
type ChangeCheckStatusRequest struct {
	ID       int64
	TenantID string
	// ResourceGroupID is the caller's scope, filled from the authenticated
	// principal rather than the request body.
	ResourceGroupID string
	Version         int `json:"version" binding:"required,min=1"`
}

func (r ChangeCheckStatusRequest) ToChangeStatusInput() checkcommand.ChangeStatusInput {
	return checkcommand.ChangeStatusInput{
		ID:              r.ID,
		TenantID:        r.TenantID,
		ResourceGroupID: r.ResourceGroupID,
		Version:         r.Version,
	}
}

// ListCheckRunsRequest defines the query parameters for listing check runs.
type ListCheckRunsRequest struct {
	TenantID string `form:"tenant_id" binding:"omitempty,len=26"`
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
	Name        string  `json:"name" binding:"required,max=128"`
	Description *string `json:"description,omitempty"`
	SLIType     string  `json:"sli_type" binding:"required,oneof=availability latency quality custom"`
	// source_type only admits job_run: SLI events derive from the run ledger,
	// naming their source definition by source_uid in source_config. The old
	// check_run source read a work queue that no longer executes anything.
	SourceType        string         `json:"source_type" binding:"omitempty,oneof=job_run"`
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
	WindowType        string  `json:"window_type" binding:"omitempty,oneof=rolling calendar quarterly"`
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
	TenantID string  `form:"tenant_id"`
	SLOID    *int64  `form:"slo_id,omitempty"`
	Status   *string `form:"status,omitempty"`
	Limit    int     `form:"limit,default=50" binding:"min=1,max=100"`
	Offset   int     `form:"offset,default=0" binding:"min=0"`
}

func (r ListSLOAlertsRequest) ToListInput() sloalertquery.ListInput {
	return sloalertquery.ListInput{
		SLOID:  r.SLOID,
		Status: r.Status,
		Limit:  r.Limit,
		Offset: r.Offset,
	}
}

// ==================== Tenant Requests ====================

// CreateTenantRequest defines the HTTP payload for creating a tenant.
type CreateTenantRequest struct {
	Slug   string `json:"slug" binding:"required,max=64"`
	Name   string `json:"name" binding:"required,max=128"`
	Status string `json:"status" binding:"omitempty,oneof=active suspended"`
}

// ToCreateInput converts the HTTP request into an admin command input.
func (r CreateTenantRequest) ToCreateInput() tenantcommand.CreateInput {
	return tenantcommand.CreateInput{
		Slug:   r.Slug,
		Name:   r.Name,
		Status: r.Status,
	}
}

// ListTenantsRequest defines the query parameters for listing tenants.
type ListTenantsRequest struct {
	Limit  int `form:"limit" binding:"omitempty,min=1,max=100"`
	Offset int `form:"offset" binding:"omitempty,min=0"`
}

// ToListInput converts the HTTP query parameters into a control-plane query input.
func (r ListTenantsRequest) ToListInput() tenantquery.ListInput {
	return tenantquery.ListInput{
		Limit:  r.Limit,
		Offset: r.Offset,
	}
}

// TenantURI defines the route parameters for reading one tenant.
type TenantURI struct {
	ID string `uri:"id" binding:"required,max=26"`
}

// ==================== API Keys ====================

// CreateAPIKeyRequest defines the HTTP payload for creating an API key.
//
// All three fields are optional: a bare request mints a key with no grants,
// which can authenticate but reach no guarded route.
type CreateAPIKeyRequest struct {
	// Policies names the policy documents the new key carries.
	Policies []string `json:"policies"`
	// BoundaryPolicyID caps what those policies can ever grant.
	BoundaryPolicyID string `json:"boundary_policy_id"`
	// ResourceGroupID scopes the key to one resource group.
	ResourceGroupID string `json:"resource_group_id"`
}

// ToCreateInput converts the HTTP request into an admin command input. The
// caller is passed through because what may be granted depends on what the
// granter already holds.
func (r CreateAPIKeyRequest) ToCreateInput(tenantID string, caller apikeycommand.Caller) apikeycommand.CreateInput {
	return apikeycommand.CreateInput{
		TenantID:         tenantID,
		PolicyIDs:        r.Policies,
		BoundaryPolicyID: r.BoundaryPolicyID,
		ResourceGroupID:  r.ResourceGroupID,
		Caller:           caller,
	}
}

// APIKeyURI defines the route parameters for revoking one API key.
type APIKeyURI struct {
	ID string `uri:"id" binding:"required,max=26"`
}

// ==================== Policies ====================

// CreatePolicyRequest defines the HTTP payload for creating a policy.
//
// The document is carried raw rather than decoded into policy.Document: the
// domain parses and validates the submitted bytes, and those same bytes are
// what gets stored. Decoding and re-encoding would store a document nobody
// checked.
type CreatePolicyRequest struct {
	Name        string          `json:"name" binding:"required,max=128"`
	Description string          `json:"description" binding:"omitempty,max=2000"`
	Document    json.RawMessage `json:"document" binding:"required"`
}

// ToCreateInput converts the HTTP request into an admin command input.
func (r CreatePolicyRequest) ToCreateInput(tenantID, actorID string) policycommand.CreateInput {
	return policycommand.CreateInput{
		TenantID:    tenantID,
		Name:        r.Name,
		Description: r.Description,
		Document:    r.Document,
		ActorID:     actorID,
	}
}

// PolicyURI defines the route parameters for reading or deleting one policy.
type PolicyURI struct {
	ID string `uri:"id" binding:"required,max=26"`
}

// ==================== Resource groups ====================

// CreateResourceGroupRequest defines the HTTP payload for creating a group.
type CreateResourceGroupRequest struct {
	Slug string `json:"slug" binding:"required,max=64"`
	Name string `json:"name" binding:"required,max=128"`
}

// ToCreateInput converts the HTTP request into an admin command input.
func (r CreateResourceGroupRequest) ToCreateInput(tenantID, actorID string) resourcegroupcommand.CreateInput {
	return resourcegroupcommand.CreateInput{
		TenantID: tenantID,
		Slug:     r.Slug,
		Name:     r.Name,
		ActorID:  actorID,
	}
}

// ==================== Function Requests ====================

// ListFunctionsRequest defines the query parameters for listing function
// definitions. There is no status filter in v1 for the same reason jobs have
// none: what a caller can address is the live row, and paused is a property
// the body already carries.
type ListFunctionsRequest struct {
	TenantID string `form:"tenant_id" binding:"omitempty,len=26"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Offset   int    `form:"offset" binding:"omitempty,min=0"`
}

// GetFunctionRequest defines the route and query parameters for reading one
// function definition.
type GetFunctionRequest struct {
	ID       int64  `uri:"id" binding:"required,min=1"`
	TenantID string `form:"tenant_id" binding:"omitempty,len=26"`
}

// InvokeFunctionRequest defines the route and query parameters for an
// invocation. wait_seconds is the synchronous variant's budget: zero (or
// absent) means async-with-reference, and the cap keeps one HTTP request from
// being held open past the platform's honest latency promise.
type InvokeFunctionRequest struct {
	ID          int64 `uri:"id" binding:"required,min=1"`
	WaitSeconds int   `form:"wait_seconds" binding:"omitempty,min=0,max=60"`
}

// ListFunctionRunsRequest defines the route and query parameters for listing
// one function's terminal invocations.
type ListFunctionRunsRequest struct {
	ID       int64  `uri:"id" binding:"required,min=1"`
	TenantID string `form:"tenant_id" binding:"omitempty,len=26"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=100"`
}

// GetFunctionRunRequest defines the route parameters for reading one terminal
// invocation. The run id is the read model's deterministic UUID, derived from
// the ledger occurrence key, so a replayed recording addresses the same row.
type GetFunctionRunRequest struct {
	ID    int64  `uri:"id" binding:"required,min=1"`
	RunID string `uri:"run_id" binding:"required,max=64"`
}

// ==================== Workflow Requests ====================

// ListWorkflowsRequest defines the query parameters for listing workflow
// definitions.
type ListWorkflowsRequest struct {
	TenantID string `form:"tenant_id" binding:"omitempty,len=26"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Offset   int    `form:"offset" binding:"omitempty,min=0"`
}

// GetWorkflowRequest defines the route and query parameters for reading one
// workflow definition. The id is the active revision id, the jobs convention.
type GetWorkflowRequest struct {
	ID       int64  `uri:"id" binding:"required,min=1"`
	TenantID string `form:"tenant_id" binding:"omitempty,len=26"`
}

// ListWorkflowRunsRequest defines the route and query parameters for listing
// one workflow's runs.
type ListWorkflowRunsRequest struct {
	ID       int64  `uri:"id" binding:"required,min=1"`
	TenantID string `form:"tenant_id" binding:"omitempty,len=26"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Offset   int    `form:"offset" binding:"omitempty,min=0"`
}

// workflowRunIDURI addresses one run under one workflow: the workflow by its
// active revision id, the run by its ledger id.
type workflowRunIDURI struct {
	ID    int64 `uri:"id" binding:"required,min=1"`
	RunID int64 `uri:"run_id" binding:"required,min=1"`
}

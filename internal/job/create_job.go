package job

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

const (
	DefaultTenantID   = "default"
	DefaultTimezone   = "UTC"
	DefaultTimeoutSec = 60

	TriggerTypeCron   = "cron"
	TriggerTypeManual = "manual"

	RetryBackoffFixed       = "fixed"
	RetryBackoffExponential = "exponential"

	ConcurrencyAllow   = "allow"
	ConcurrencyForbid  = "forbid"
	ConcurrencyReplace = "replace"

	MisfireSkip    = "skip"
	MisfireFireNow = "fire_now"
	MisfireCatchUp = "catch_up"
)

// CreateJobInput is the internal input used by the domain layer.
// It intentionally does not carry JSON or binding tags.
type CreateJobInput struct {
	Name                 string
	TenantID             string
	TriggerType          string
	CronExpr             *string
	Timezone             string
	HandlerType          string
	HandlerPayload       map[string]any
	TimeoutSec           int
	RetryLimit           int
	RetryBackoffSec      int
	RetryBackoffStrategy string
	ConcurrencyPolicy    string
	MisfirePolicy        string
}

// CreateJobSpec is the normalized result after defaulting and validation.
// Repository code should persist this instead of raw request input.
type CreateJobSpec struct {
	Name                 string
	TenantID             string
	TriggerType          string
	CronExpr             *string
	Timezone             string
	HandlerType          string
	HandlerPayload       map[string]any
	TimeoutSec           int
	RetryLimit           int
	RetryBackoffSec      int
	RetryBackoffStrategy string
	ConcurrencyPolicy    string
	MisfirePolicy        string
	NextRunAt            *time.Time
}

func NewCreateJobInput(req CreateJobRequest) CreateJobInput {
	return CreateJobInput{
		Name:                 req.Name,
		TenantID:             req.TenantID,
		TriggerType:          req.TriggerType,
		CronExpr:             req.CronExpr,
		Timezone:             req.Timezone,
		HandlerType:          req.HandlerType,
		HandlerPayload:       req.HandlerPayload,
		TimeoutSec:           req.TimeoutSec,
		RetryLimit:           req.RetryLimit,
		RetryBackoffSec:      req.RetryBackoffSec,
		RetryBackoffStrategy: req.RetryBackoffStrategy,
		ConcurrencyPolicy:    req.ConcurrencyPolicy,
		MisfirePolicy:        req.MisfirePolicy,
	}
}

func NormalizeCreateJob(now time.Time, in CreateJobInput) (CreateJobSpec, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return CreateJobSpec{}, fmt.Errorf("name is required")
	}
	if len(name) > 128 {
		return CreateJobSpec{}, fmt.Errorf("name must be <= 128 characters")
	}

	handlerType := strings.TrimSpace(in.HandlerType)
	if handlerType == "" {
		return CreateJobSpec{}, fmt.Errorf("handler_type is required")
	}
	if len(handlerType) > 32 {
		return CreateJobSpec{}, fmt.Errorf("handler_type must be <= 32 characters")
	}

	triggerType := strings.TrimSpace(in.TriggerType)
	if !isOneOf(triggerType, TriggerTypeCron, TriggerTypeManual) {
		return CreateJobSpec{}, fmt.Errorf("trigger_type must be one of: %s, %s", TriggerTypeCron, TriggerTypeManual)
	}

	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	if len(tenantID) > 64 {
		return CreateJobSpec{}, fmt.Errorf("tenant_id must be <= 64 characters")
	}

	timezone := strings.TrimSpace(in.Timezone)
	if timezone == "" {
		timezone = DefaultTimezone
	}
	if len(timezone) > 64 {
		return CreateJobSpec{}, fmt.Errorf("timezone must be <= 64 characters")
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return CreateJobSpec{}, fmt.Errorf("invalid timezone: %w", err)
	}

	timeoutSec := in.TimeoutSec
	if timeoutSec == 0 {
		timeoutSec = DefaultTimeoutSec
	}
	if timeoutSec < 1 {
		return CreateJobSpec{}, fmt.Errorf("timeout_sec must be >= 1")
	}

	if in.RetryLimit < 0 {
		return CreateJobSpec{}, fmt.Errorf("retry_limit must be >= 0")
	}
	if in.RetryBackoffSec < 0 {
		return CreateJobSpec{}, fmt.Errorf("retry_backoff_sec must be >= 0")
	}

	retryBackoffStrategy := strings.TrimSpace(in.RetryBackoffStrategy)
	if retryBackoffStrategy == "" {
		retryBackoffStrategy = RetryBackoffFixed
	}
	if !isOneOf(retryBackoffStrategy, RetryBackoffFixed, RetryBackoffExponential) {
		return CreateJobSpec{}, fmt.Errorf("invalid retry_backoff_strategy. Valid options: (fixed, exponential)")
	}

	concurrencyPolicy := strings.TrimSpace(in.ConcurrencyPolicy)
	if concurrencyPolicy == "" {
		concurrencyPolicy = ConcurrencyAllow
	}
	if !isOneOf(concurrencyPolicy, ConcurrencyAllow, ConcurrencyForbid, ConcurrencyReplace) {
		return CreateJobSpec{}, fmt.Errorf("invalid concurrency_policy. Valid options: (allow, forbid, replace)")
	}
	misfirePolicy := strings.TrimSpace(in.MisfirePolicy)
	if misfirePolicy == "" {
		misfirePolicy = MisfireSkip
	}
	if !isOneOf(misfirePolicy, MisfireSkip, MisfireFireNow, MisfireCatchUp) {
		return CreateJobSpec{}, fmt.Errorf("invalid misfire_policy. Valid options: (skip, fire_now, catch_up)")
	}

	payload := cloneHandlerPayload(in.HandlerPayload)

	var cronExpr *string
	var nextRunAt *time.Time

	if triggerType == TriggerTypeManual && in.CronExpr != nil && strings.TrimSpace(*in.CronExpr) != "" {
		return CreateJobSpec{}, fmt.Errorf("cron_expr must be empty for manual jobs")
	}
	if triggerType == TriggerTypeCron {
		if in.CronExpr == nil || strings.TrimSpace(*in.CronExpr) == "" {
			return CreateJobSpec{}, fmt.Errorf("cron_expr is required for cron jobs")
		}

		expr := strings.TrimSpace(*in.CronExpr)
		if len(expr) > 64 {
			return CreateJobSpec{}, fmt.Errorf("cron_expr must be <= 64 characters")
		}

		schedule, err := cron.ParseStandard(expr)
		if err != nil {
			return CreateJobSpec{}, fmt.Errorf("invalid cron_expr: %w", err)
		}

		next := schedule.Next(now.In(loc)).UTC()
		cronExpr = &expr
		nextRunAt = &next
	}

	return CreateJobSpec{
		Name:                 name,
		TenantID:             tenantID,
		TriggerType:          triggerType,
		CronExpr:             cronExpr,
		Timezone:             timezone,
		HandlerType:          handlerType,
		HandlerPayload:       payload,
		TimeoutSec:           timeoutSec,
		RetryLimit:           in.RetryLimit,
		RetryBackoffSec:      in.RetryBackoffSec,
		RetryBackoffStrategy: retryBackoffStrategy,
		ConcurrencyPolicy:    concurrencyPolicy,
		MisfirePolicy:        misfirePolicy,
		NextRunAt:            nextRunAt,
	}, nil
}

func isOneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func cloneHandlerPayload(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}

	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

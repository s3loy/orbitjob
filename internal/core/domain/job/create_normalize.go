package job

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

func NormalizeCreate(now time.Time, in CreateInput) (CreateSpec, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return CreateSpec{}, validationError("name", "is required")
	}
	if len(name) > 128 {
		return CreateSpec{}, validationError("name", "must be <= 128 characters")
	}

	handlerType := strings.TrimSpace(in.HandlerType)
	if handlerType == "" {
		return CreateSpec{}, validationError("handler_type", "is required")
	}
	if len(handlerType) > 32 {
		return CreateSpec{}, validationError("handler_type", "must be <= 32 characters")
	}

	triggerType := strings.TrimSpace(in.TriggerType)
	if !isOneOf(triggerType, TriggerTypeCron, TriggerTypeManual) {
		return CreateSpec{}, validationErrorf("trigger_type", "must be one of: %s, %s", TriggerTypeCron, TriggerTypeManual)
	}

	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	if len(tenantID) > 64 {
		return CreateSpec{}, validationError("tenant_id", "must be <= 64 characters")
	}

	priority := in.Priority
	if priority < 0 {
		return CreateSpec{}, validationError("priority", "must be >= 0")
	}

	partitionKey, err := normalizeOptionalString(in.PartitionKey, "partition_key", 64)
	if err != nil {
		return CreateSpec{}, err
	}

	timezone := strings.TrimSpace(in.Timezone)
	if timezone == "" {
		timezone = DefaultTimezone
	}
	if len(timezone) > 64 {
		return CreateSpec{}, validationError("timezone", "must be <= 64 characters")
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return CreateSpec{}, &ValidationError{
			Field:   "timezone",
			Message: "invalid timezone",
			Cause:   err,
		}
	}

	timeoutSec := in.TimeoutSec
	if timeoutSec == 0 {
		timeoutSec = DefaultTimeoutSec
	}
	if timeoutSec < 1 {
		return CreateSpec{}, validationError("timeout_sec", "must be >= 1")
	}

	if in.RetryLimit < 0 {
		return CreateSpec{}, validationError("retry_limit", "must be >= 0")
	}
	if in.RetryBackoffSec < 0 {
		return CreateSpec{}, validationError("retry_backoff_sec", "must be >= 0")
	}

	retryBackoffStrategy := strings.TrimSpace(in.RetryBackoffStrategy)
	if retryBackoffStrategy == "" {
		retryBackoffStrategy = RetryBackoffFixed
	}
	if !isOneOf(retryBackoffStrategy, RetryBackoffFixed, RetryBackoffExponential) {
		return CreateSpec{}, validationError("retry_backoff_strategy", "must be one of: fixed, exponential")
	}

	concurrencyPolicy := strings.TrimSpace(in.ConcurrencyPolicy)
	if concurrencyPolicy == "" {
		concurrencyPolicy = ConcurrencyAllow
	}
	if !isOneOf(concurrencyPolicy, ConcurrencyAllow, ConcurrencyForbid, ConcurrencyReplace) {
		return CreateSpec{}, validationError("concurrency_policy", "must be one of: allow, forbid, replace")
	}

	misfirePolicy := strings.TrimSpace(in.MisfirePolicy)
	if misfirePolicy == "" {
		misfirePolicy = MisfireSkip
	}
	if !isOneOf(misfirePolicy, MisfireSkip, MisfireFireNow, MisfireCatchUp) {
		return CreateSpec{}, validationError("misfire_policy", "must be one of: skip, fire_now, catch_up")
	}

	payload := cloneHandlerPayload(in.HandlerPayload)
	if err := validateHandlerPayload(payload); err != nil {
		return CreateSpec{}, err
	}

	var cronExpr *string
	var nextRunAt *time.Time

	if triggerType == TriggerTypeManual && in.CronExpr != nil && strings.TrimSpace(*in.CronExpr) != "" {
		return CreateSpec{}, validationError("cron_expr", "must be empty for manual jobs")
	}
	if triggerType == TriggerTypeCron {
		if in.CronExpr == nil || strings.TrimSpace(*in.CronExpr) == "" {
			return CreateSpec{}, validationError("cron_expr", "is required for cron jobs")
		}

		expr := strings.TrimSpace(*in.CronExpr)
		if len(expr) > 64 {
			return CreateSpec{}, validationError("cron_expr", "must be <= 64 characters")
		}

		schedule, err := cron.ParseStandard(expr)
		if err != nil {
			return CreateSpec{}, &ValidationError{
				Field:   "cron_expr",
				Message: "invalid cron_expr",
				Cause:   err,
			}
		}

		next := schedule.Next(now.In(loc)).UTC()
		cronExpr = &expr
		nextRunAt = &next
	}

	return CreateSpec{
		Name:                 name,
		TenantID:             tenantID,
		Priority:             priority,
		PartitionKey:         partitionKey,
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

func validateHandlerPayload(in map[string]any) error {
	if in == nil {
		return nil
	}

	if _, err := json.Marshal(in); err != nil {
		return &ValidationError{
			Field:   "handler_payload",
			Message: "must be JSON serializable",
			Cause:   err,
		}
	}

	return nil
}

func normalizeOptionalString(in *string, field string, maxLen int) (*string, error) {
	if in == nil {
		return nil, nil
	}

	value := strings.TrimSpace(*in)
	if value == "" {
		return nil, nil
	}
	if len(value) > maxLen {
		return nil, validationErrorf(field, "must be <= %d characters", maxLen)
	}

	return &value, nil
}

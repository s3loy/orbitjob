package job

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

func NormalizeCreate(now time.Time, in CreateInput) (CreateSpec, error) {
	name, err := normalizeRequiredString(in.Name, "name", 128)
	if err != nil {
		return CreateSpec{}, err
	}

	handlerType, err := normalizeRequiredString(in.HandlerType, "handler_type", 32)
	if err != nil {
		return CreateSpec{}, err
	}

	triggerType, err := normalizeTriggerType(in.TriggerType)
	if err != nil {
		return CreateSpec{}, err
	}

	tenantID, err := normalizeTenantID(in.TenantID)
	if err != nil {
		return CreateSpec{}, err
	}

	priority, err := normalizePriority(in.Priority)
	if err != nil {
		return CreateSpec{}, err
	}

	partitionKey, err := normalizeOptionalString(in.PartitionKey, "partition_key", 64)
	if err != nil {
		return CreateSpec{}, err
	}

	timezone, loc, err := normalizeTimezone(in.Timezone)
	if err != nil {
		return CreateSpec{}, err
	}

	timeoutSec, err := normalizeTimeoutSec(in.TimeoutSec)
	if err != nil {
		return CreateSpec{}, err
	}

	retryLimit, retryBackoffSec, retryBackoffStrategy, err := normalizeRetrySettings(
		in.RetryLimit,
		in.RetryBackoffSec,
		in.RetryBackoffStrategy,
	)
	if err != nil {
		return CreateSpec{}, err
	}

	concurrencyPolicy, misfirePolicy, err := normalizeExecutionPolicies(
		in.ConcurrencyPolicy,
		in.MisfirePolicy,
	)
	if err != nil {
		return CreateSpec{}, err
	}

	payload := cloneHandlerPayload(in.HandlerPayload)
	if err := validateHandlerPayload(payload); err != nil {
		return CreateSpec{}, err
	}

	cronExpr, nextRunAt, err := normalizeSchedule(now, loc, triggerType, in.CronExpr)
	if err != nil {
		return CreateSpec{}, err
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
		RetryLimit:           retryLimit,
		RetryBackoffSec:      retryBackoffSec,
		RetryBackoffStrategy: retryBackoffStrategy,
		ConcurrencyPolicy:    concurrencyPolicy,
		MisfirePolicy:        misfirePolicy,
		NextRunAt:            nextRunAt,
	}, nil
}

func normalizeRequiredString(in string, field string, maxLen int) (string, error) {
	value := strings.TrimSpace(in)
	if value == "" {
		return "", validationError(field, "is required")
	}
	if len(value) > maxLen {
		return "", validationErrorf(field, "must be <= %d characters", maxLen)
	}

	return value, nil
}

func normalizeTriggerType(in string) (string, error) {
	value := strings.TrimSpace(in)
	if !isOneOf(value, TriggerTypeCron, TriggerTypeManual) {
		return "", validationErrorf("trigger_type", "must be one of: %s, %s", TriggerTypeCron, TriggerTypeManual)
	}

	return value, nil
}

func normalizeTenantID(in string) (string, error) {
	value := strings.TrimSpace(in)
	if value == "" {
		value = DefaultTenantID
	}
	if len(value) > 64 {
		return "", validationError("tenant_id", "must be <= 64 characters")
	}

	return value, nil
}

func normalizePriority(in int) (int, error) {
	if in < 0 {
		return 0, validationError("priority", "must be >= 0")
	}

	return in, nil
}

func normalizeTimezone(in string) (string, *time.Location, error) {
	value := strings.TrimSpace(in)
	if value == "" {
		value = DefaultTimezone
	}
	if len(value) > 64 {
		return "", nil, validationError("timezone", "must be <= 64 characters")
	}

	loc, err := time.LoadLocation(value)
	if err != nil {
		return "", nil, &ValidationError{
			Field:   "timezone",
			Message: "invalid timezone",
			Cause:   err,
		}
	}

	return value, loc, nil
}

func normalizeTimeoutSec(in int) (int, error) {
	value := in
	if value == 0 {
		value = DefaultTimeoutSec
	}
	if value < 1 {
		return 0, validationError("timeout_sec", "must be >= 1")
	}

	return value, nil
}

func normalizeRetrySettings(retryLimit int, retryBackoffSec int, retryBackoffStrategy string) (int, int, string, error) {
	if retryLimit < 0 {
		return 0, 0, "", validationError("retry_limit", "must be >= 0")
	}
	if retryBackoffSec < 0 {
		return 0, 0, "", validationError("retry_backoff_sec", "must be >= 0")
	}

	strategy := strings.TrimSpace(retryBackoffStrategy)
	if strategy == "" {
		strategy = RetryBackoffFixed
	}
	if !isOneOf(strategy, RetryBackoffFixed, RetryBackoffExponential) {
		return 0, 0, "", validationError("retry_backoff_strategy", "must be one of: fixed, exponential")
	}

	return retryLimit, retryBackoffSec, strategy, nil
}

func normalizeExecutionPolicies(concurrencyPolicy string, misfirePolicy string) (string, string, error) {
	normalizedConcurrency := strings.TrimSpace(concurrencyPolicy)
	if normalizedConcurrency == "" {
		normalizedConcurrency = ConcurrencyAllow
	}
	if !isOneOf(normalizedConcurrency, ConcurrencyAllow, ConcurrencyForbid, ConcurrencyReplace) {
		return "", "", validationError("concurrency_policy", "must be one of: allow, forbid, replace")
	}

	normalizedMisfire := strings.TrimSpace(misfirePolicy)
	if normalizedMisfire == "" {
		normalizedMisfire = MisfireSkip
	}
	if !isOneOf(normalizedMisfire, MisfireSkip, MisfireFireNow, MisfireCatchUp) {
		return "", "", validationError("misfire_policy", "must be one of: skip, fire_now, catch_up")
	}

	return normalizedConcurrency, normalizedMisfire, nil
}

func normalizeSchedule(now time.Time, loc *time.Location, triggerType string, cronExpr *string) (*string, *time.Time, error) {
	if triggerType == TriggerTypeManual {
		if cronExpr != nil && strings.TrimSpace(*cronExpr) != "" {
			return nil, nil, validationError("cron_expr", "must be empty for manual jobs")
		}

		return nil, nil, nil
	}

	if cronExpr == nil || strings.TrimSpace(*cronExpr) == "" {
		return nil, nil, validationError("cron_expr", "is required for cron jobs")
	}

	expr := strings.TrimSpace(*cronExpr)
	if len(expr) > 64 {
		return nil, nil, validationError("cron_expr", "must be <= 64 characters")
	}

	schedule, err := cron.ParseStandard(expr)
	if err != nil {
		return nil, nil, &ValidationError{
			Field:   "cron_expr",
			Message: "invalid cron_expr",
			Cause:   err,
		}
	}

	next := schedule.Next(now.In(loc)).UTC()
	return &expr, &next, nil
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

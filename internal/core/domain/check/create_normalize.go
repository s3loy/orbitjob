package check

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

	description, err := normalizeOptionalString(in.Description, "description", 512)
	if err != nil {
		return CreateSpec{}, err
	}

	tenantID, err := normalizeTenantID(in.TenantID)
	if err != nil {
		return CreateSpec{}, err
	}

	checkType, err := normalizeCheckType(in.CheckType)
	if err != nil {
		return CreateSpec{}, err
	}

	checkConfig, err := normalizeJSONB(in.CheckConfig, "check_config")
	if err != nil {
		return CreateSpec{}, err
	}

	assertionRules, err := normalizeAssertionRules(in.AssertionRules)
	if err != nil {
		return CreateSpec{}, err
	}

	scheduleType, err := normalizeScheduleType(in.ScheduleType)
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

	retryLimit, err := normalizeRetryLimit(in.RetryLimit)
	if err != nil {
		return CreateSpec{}, err
	}

	priority, err := normalizePriority(in.Priority)
	if err != nil {
		return CreateSpec{}, err
	}

	labels, err := normalizeJSONB(in.Labels, "labels")
	if err != nil {
		return CreateSpec{}, err
	}

	cronExpr, intervalSec, nextRunAt, err := normalizeSchedule(now, loc, scheduleType, in.CronExpr, in.IntervalSec)
	if err != nil {
		return CreateSpec{}, err
	}

	return CreateSpec{
		Name:           name,
		Description:    description,
		TenantID:       tenantID,
		CheckType:      checkType,
		CheckConfig:    checkConfig,
		AssertionRules: assertionRules,
		ScheduleType:   scheduleType,
		CronExpr:       cronExpr,
		IntervalSec:    intervalSec,
		Timezone:       timezone,
		TimeoutSec:     timeoutSec,
		RetryLimit:     retryLimit,
		Priority:       priority,
		Labels:         labels,
		NextRunAt:      nextRunAt,
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

func normalizeCheckType(in string) (string, error) {
	value := strings.TrimSpace(in)
	if value == "" {
		return "", validationError("check_type", "is required")
	}
	if !isOneOf(value, CheckTypeHTTPHealth) {
		return "", validationErrorf("check_type", "must be one of: %s", CheckTypeHTTPHealth)
	}
	return value, nil
}

func normalizeJSONB(in map[string]any, field string) (map[string]any, error) {
	if in == nil {
		return map[string]any{}, nil
	}
	if _, err := json.Marshal(in); err != nil {
		return nil, validationErrorf(field, "must be JSON serializable")
	}
	return in, nil
}

func normalizeAssertionRules(in []AssertionRule) ([]AssertionRule, error) {
	if len(in) == 0 {
		return []AssertionRule{}, nil
	}

	out := make([]AssertionRule, len(in))
	for i, rule := range in {
		if strings.TrimSpace(rule.Metric) == "" {
			return nil, validationErrorf("assertion_rules[%d].metric", "is required")
		}
		if len(rule.Metric) > 64 {
			return nil, validationErrorf("assertion_rules[%d].metric", "must be <= 64 characters")
		}

		operator := strings.TrimSpace(rule.Operator)
		if !isOneOf(operator, ">", "<", "==", "!=", ">=", "<=") {
			return nil, validationErrorf("assertion_rules[%d].operator", "must be one of: >, <, ==, !=, >=, <=")
		}

		severity := strings.TrimSpace(rule.Severity)
		if !isOneOf(severity, "warning", "critical") {
			return nil, validationErrorf("assertion_rules[%d].severity", "must be one of: warning, critical")
		}

		out[i] = AssertionRule{
			Metric:    rule.Metric,
			Operator:  operator,
			Threshold: rule.Threshold,
			Severity:  severity,
		}
	}
	return out, nil
}

func normalizeScheduleType(in string) (string, error) {
	value := strings.TrimSpace(in)
	if value == "" {
		value = ScheduleTypeCron
	}
	if !isOneOf(value, ScheduleTypeCron, ScheduleTypeInterval) {
		return "", validationError("schedule_type", "must be one of: cron, interval")
	}
	return value, nil
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
	if in == 0 {
		in = DefaultTimeoutSec
	}
	if in < 1 {
		return 0, validationError("timeout_sec", "must be >= 1")
	}
	return in, nil
}

func normalizeRetryLimit(in int) (int, error) {
	if in < 0 {
		return 0, validationError("retry_limit", "must be >= 0")
	}
	if in == 0 {
		in = DefaultRetryLimit
	}
	return in, nil
}

func normalizePriority(in int) (int, error) {
	if in < 0 {
		return 0, validationError("priority", "must be >= 0")
	}
	return in, nil
}

func normalizeSchedule(now time.Time, loc *time.Location, scheduleType string, cronExpr *string, intervalSec *int) (*string, *int, *time.Time, error) {
	switch scheduleType {
	case ScheduleTypeCron:
		if cronExpr == nil || strings.TrimSpace(*cronExpr) == "" {
			return nil, nil, nil, validationError("cron_expr", "is required for cron schedule")
		}
		expr := strings.TrimSpace(*cronExpr)
		if len(expr) > 128 {
			return nil, nil, nil, validationError("cron_expr", "must be <= 128 characters")
		}
		schedule, err := cron.ParseStandard(expr)
		if err != nil {
			return nil, nil, nil, &ValidationError{
				Field:   "cron_expr",
				Message: "invalid cron expression",
				Cause:   err,
			}
		}
		next := schedule.Next(now.In(loc)).UTC()
		return &expr, nil, &next, nil

	case ScheduleTypeInterval:
		if intervalSec == nil || *intervalSec < 1 {
			return nil, nil, nil, validationError("interval_sec", "is required and must be >= 1 for interval schedule")
		}
		next := now.Add(time.Duration(*intervalSec) * time.Second).UTC()
		return nil, intervalSec, &next, nil
	}

	return nil, nil, nil, validationError("schedule_type", "invalid schedule type")
}

func isOneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

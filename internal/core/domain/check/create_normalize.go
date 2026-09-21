package check

import (
	"encoding/json"
	"strconv"
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

	// The probe rendering executes exactly what check_config describes, so the
	// admin boundary enforces the http_health shape here rather than letting a
	// malformed config surface later as a Kubernetes Job that fails opaquely.
	if checkType == CheckTypeHTTPHealth {
		if _, err := NormalizeProbeConfig(checkConfig); err != nil {
			return CreateSpec{}, err
		}
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
		Name:            name,
		Description:     description,
		TenantID:        tenantID,
		ResourceGroupID: strings.TrimSpace(in.ResourceGroupID),
		CheckType:       checkType,
		CheckConfig:     checkConfig,
		AssertionRules:  assertionRules,
		ScheduleType:    scheduleType,
		CronExpr:        cronExpr,
		IntervalSec:     intervalSec,
		Timezone:        timezone,
		TimeoutSec:      timeoutSec,
		RetryLimit:      retryLimit,
		Priority:        priority,
		Labels:          labels,
		NextRunAt:       nextRunAt,
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

// normalizeTenantID refuses anything that is not a tenant id. The schema's
// tenant_id columns are CHAR(26) ULIDs, so an empty or differently shaped value
// would create a check no tenant context could ever read or schedule; falling
// back to a hardcoded tenant would instead silently file it under whichever
// tenant that constant named.
func normalizeTenantID(in string) (string, error) {
	value := strings.TrimSpace(in)
	if value == "" {
		return "", validationError("tenant_id", "is required")
	}
	if len(value) != TenantIDLength {
		return "", validationErrorf("tenant_id", "must be exactly %d characters", TenantIDLength)
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
		metric := strings.TrimSpace(rule.Metric)
		if metric == "" {
			return nil, validationError(assertionRuleField(i, "metric"), "is required")
		}
		if len(metric) > 64 {
			return nil, validationErrorf(assertionRuleField(i, "metric"), "must be <= 64 characters")
		}

		operator := strings.TrimSpace(rule.Operator)
		if !isOneOf(operator, ">", "<", "==", "!=", ">=", "<=") {
			return nil, validationErrorf(assertionRuleField(i, "operator"), "must be one of: >, <, ==, !=, >=, <=")
		}

		severity := strings.TrimSpace(rule.Severity)
		if !isOneOf(severity, "warning", "critical") {
			return nil, validationErrorf(assertionRuleField(i, "severity"), "must be one of: warning, critical")
		}

		out[i] = AssertionRule{
			Metric:    metric,
			Operator:  operator,
			Threshold: rule.Threshold,
			Severity:  severity,
		}
	}
	return out, nil
}

// assertionRuleField names the offending rule by index. The field travels into
// the API error body, so an index without its position would point every rule
// fault at the same unadressable name.
func assertionRuleField(rule int, name string) string {
	return "assertion_rules[" + strconv.Itoa(rule) + "]." + name
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

// normalizePriority applies the same zero-means-default convention as
// normalizeTimeoutSec and normalizeRetryLimit: the schema column defaults to 5
// and the admin API documents priority as optional, so an omitted priority must
// land on DefaultPriority rather than silently ranking the check last.
func normalizePriority(in int) (int, error) {
	if in < 0 {
		return 0, validationError("priority", "must be >= 0")
	}
	if in == 0 {
		in = DefaultPriority
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
		if intervalSec == nil {
			return nil, nil, nil, validationErrorf("interval_sec", "is required and must be >= %d for interval schedule", MinimumIntervalSec)
		}
		if *intervalSec < MinimumIntervalSec {
			return nil, nil, nil, validationErrorf("interval_sec", "must be >= %d for interval schedule", MinimumIntervalSec)
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

package check

import (
	"net/url"
	"strconv"
	"strings"
)

// ProbeConfig is the http_health configuration a check executes with, after
// normalization. It is the exact field set the Kubernetes probe rendering
// consumes; anything else stored in check_config is inert metadata.
type ProbeConfig struct {
	URL            string
	Method         string
	ExpectedStatus int
}

// NormalizeProbeConfig validates the http_health shape of a check_config and
// returns the fields the probe rendering uses, with defaults applied.
//
// Two callers share this one rule set so they cannot drift: the admin create
// boundary, which must reject a config that would render a uselessly failing
// Job, and the revision synthesizer, which re-validates what is already stored
// because a row written before a validation rule existed must fail loudly at
// synthesis instead of silently producing a probe that cannot answer.
func NormalizeProbeConfig(cfg map[string]any) (ProbeConfig, error) {
	rawURL, ok := cfg["url"]
	if !ok {
		return ProbeConfig{}, validationError("check_config.url", "is required")
	}
	urlValue, ok := rawURL.(string)
	if !ok || strings.TrimSpace(urlValue) == "" {
		return ProbeConfig{}, validationError("check_config.url", "must be a non-empty string")
	}
	parsed, err := url.Parse(strings.TrimSpace(urlValue))
	if err != nil {
		return ProbeConfig{}, &ValidationError{
			Field:   "check_config.url",
			Message: "must be an absolute http(s) URL",
			Cause:   err,
		}
	}
	if parsed.Host == "" {
		return ProbeConfig{}, validationError("check_config.url", "must be an absolute http(s) URL")
	}
	if len(urlValue) > MaxProbeURLLength {
		return ProbeConfig{}, validationErrorf("check_config.url", "must be <= %d characters", MaxProbeURLLength)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ProbeConfig{}, validationError("check_config.url", "must use http or https")
	}

	method := DefaultProbeMethod
	if raw, present := cfg["method"]; present && raw != nil {
		requested, ok := raw.(string)
		if !ok {
			return ProbeConfig{}, validationError("check_config.method", "must be a string")
		}
		trimmed := strings.ToUpper(strings.TrimSpace(requested))
		if trimmed != "" {
			method = trimmed
		}
	}
	if !isOneOf(method, ProbeMethods...) {
		return ProbeConfig{}, validationErrorf("check_config.method", "must be one of: %s", strings.Join(ProbeMethods, ", "))
	}

	expected := DefaultExpectedStatus
	if raw, present := cfg["expected_status"]; present && raw != nil {
		status, err := jsonInt(raw)
		if err != nil {
			return ProbeConfig{}, validationError("check_config.expected_status", "must be an integer")
		}
		if status < MinExpectedStatus || status > MaxExpectedStatus {
			return ProbeConfig{}, validationErrorf("check_config.expected_status", "must be between %d and %d", MinExpectedStatus, MaxExpectedStatus)
		}
		expected = status
	}

	return ProbeConfig{URL: strings.TrimSpace(urlValue), Method: method, ExpectedStatus: expected}, nil
}

// jsonInt accepts the integer encodings JSON decoding produces. A number
// decoded from JSON arrives as float64 and must be integral; Go callers may
// pass any exact integer type.
func jsonInt(raw any) (int, error) {
	switch v := raw.(type) {
	case float64:
		if v != float64(int64(v)) {
			return 0, validationError("check_config.expected_status", "must be an integer")
		}
		return int(v), nil
	case int:
		return v, nil
	case int64:
		return int(v), nil
	default:
		return 0, validationError("check_config.expected_status", "must be a number")
	}
}

// SourceModeCheck is the job_definition_revisions source_mode a check is
// materialized under. Revisions are unique per (source_mode, source_uid,
// generation), so check-sourced revisions live in their own namespace and can
// never collide with CR-sourced ones.
const SourceModeCheck = "check"

// CheckSourceUID is the stable revision identity of a check: the literal
// prefix "check-" plus the check's row id. It is the same string the ledger
// stores as job_run_control_plane.source_uid and the SLI source_config names,
// so one derivation addresses the definition everywhere.
func CheckSourceUID(id int64) string {
	return "check-" + strconv.FormatInt(id, 10)
}

// CheckIDFromSourceUID reverses CheckSourceUID. The second return is false for
// any string the derivation could not have produced, so callers can route on
// it instead of string-matching a prefix.
func CheckIDFromSourceUID(sourceUID string) (int64, bool) {
	const prefix = "check-"
	if !strings.HasPrefix(sourceUID, prefix) {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(sourceUID, prefix), 10, 64)
	if err != nil || id < 1 {
		return 0, false
	}
	return id, true
}

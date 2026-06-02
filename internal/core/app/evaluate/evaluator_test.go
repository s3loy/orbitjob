package evaluate

import (
	"testing"

	"orbitjob/internal/core/domain/check"
)

func TestEvaluate_AllPass(t *testing.T) {
	e := NewEvaluator()
	output := map[string]any{"response_time_ms": 500}
	rules := []check.AssertionRule{
		{Metric: "response_time_ms", Operator: ">", Threshold: 1000, Severity: "warning"},
	}

	result := e.Evaluate(output, rules)
	if result.OverallSeverity != SeverityOK {
		t.Errorf("severity = %q, want %q", result.OverallSeverity, SeverityOK)
	}
	if result.Passed != 1 {
		t.Errorf("passed = %d, want 1", result.Passed)
	}
}

func TestEvaluate_Warning(t *testing.T) {
	e := NewEvaluator()
	output := map[string]any{"response_time_ms": 1500}
	rules := []check.AssertionRule{
		{Metric: "response_time_ms", Operator: ">", Threshold: 1000, Severity: "warning"},
	}

	result := e.Evaluate(output, rules)
	if result.OverallSeverity != SeverityWarning {
		t.Errorf("severity = %q, want %q", result.OverallSeverity, SeverityWarning)
	}
	if result.Failed != 1 {
		t.Errorf("failed = %d, want 1", result.Failed)
	}
}

func TestEvaluate_Critical(t *testing.T) {
	e := NewEvaluator()
	output := map[string]any{"status_code": 500}
	rules := []check.AssertionRule{
		{Metric: "status_code", Operator: "!=", Threshold: 200, Severity: "critical"},
	}

	result := e.Evaluate(output, rules)
	if result.OverallSeverity != SeverityCritical {
		t.Errorf("severity = %q, want %q", result.OverallSeverity, SeverityCritical)
	}
}

func TestEvaluate_MultipleRules(t *testing.T) {
	e := NewEvaluator()
	output := map[string]any{"response_time_ms": 500, "status_code": 500}
	rules := []check.AssertionRule{
		{Metric: "response_time_ms", Operator: ">", Threshold: 1000, Severity: "warning"},
		{Metric: "status_code", Operator: "!=", Threshold: 200, Severity: "critical"},
	}

	result := e.Evaluate(output, rules)
	if result.OverallSeverity != SeverityCritical {
		t.Errorf("severity = %q, want %q", result.OverallSeverity, SeverityCritical)
	}
	if result.Passed != 1 {
		t.Errorf("passed = %d, want 1", result.Passed)
	}
	if result.Failed != 1 {
		t.Errorf("failed = %d, want 1", result.Failed)
	}
}

func TestEvaluate_MissingMetric(t *testing.T) {
	e := NewEvaluator()
	output := map[string]any{}
	rules := []check.AssertionRule{
		{Metric: "response_time_ms", Operator: ">", Threshold: 1000, Severity: "warning"},
	}

	result := e.Evaluate(output, rules)
	if result.OverallSeverity != SeverityUnknown {
		t.Errorf("severity = %q, want %q", result.OverallSeverity, SeverityUnknown)
	}
}

func TestEvaluate_NoRules(t *testing.T) {
	e := NewEvaluator()
	result := e.Evaluate(map[string]any{"x": 1}, nil)
	if result.OverallSeverity != SeverityOK {
		t.Errorf("severity = %q, want %q", result.OverallSeverity, SeverityOK)
	}
}

func TestEvaluate_UnsupportedType(t *testing.T) {
	e := NewEvaluator()
	output := map[string]any{"metric": "not-a-number"}
	rules := []check.AssertionRule{
		{Metric: "metric", Operator: ">", Threshold: 100, Severity: "warning"},
	}

	result := e.Evaluate(output, rules)
	if result.OverallSeverity != SeverityUnknown {
		t.Errorf("severity = %q, want %q", result.OverallSeverity, SeverityUnknown)
	}
}

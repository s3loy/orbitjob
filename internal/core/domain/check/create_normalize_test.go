package check

import (
	"testing"
	"time"
)

func TestNormalizeCreate_Valid(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cron := "*/5 * * * *"

	spec, err := NormalizeCreate(now, CreateInput{
		Name:         "test-check",
		CheckType:    CheckTypeHTTPHealth,
		ScheduleType: ScheduleTypeCron,
		CronExpr:     &cron,
		CheckConfig:  map[string]any{"url": "http://example.com"},
		AssertionRules: []AssertionRule{
			{Metric: "status_code", Operator: "==", Threshold: 200, Severity: "warning"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Name != "test-check" {
		t.Errorf("name = %q, want %q", spec.Name, "test-check")
	}
	if spec.CheckType != CheckTypeHTTPHealth {
		t.Errorf("check_type = %q, want %q", spec.CheckType, CheckTypeHTTPHealth)
	}
	if spec.NextRunAt == nil {
		t.Error("expected NextRunAt to be set")
	}
}

func TestNormalizeCreate_InvalidCheckType(t *testing.T) {
	_, err := NormalizeCreate(time.Now(), CreateInput{
		Name:      "test",
		CheckType: "unknown",
	})
	if err == nil {
		t.Error("expected error for invalid check_type")
	}
}

func TestNormalizeCreate_InvalidAssertionRule(t *testing.T) {
	_, err := NormalizeCreate(time.Now(), CreateInput{
		Name:      "test",
		CheckType: CheckTypeHTTPHealth,
		AssertionRules: []AssertionRule{
			{Metric: "", Operator: ">", Threshold: 100, Severity: "warning"},
		},
	})
	if err == nil {
		t.Error("expected error for empty metric")
	}
}

func TestNormalizeCreate_InvalidOperator(t *testing.T) {
	_, err := NormalizeCreate(time.Now(), CreateInput{
		Name:      "test",
		CheckType: CheckTypeHTTPHealth,
		AssertionRules: []AssertionRule{
			{Metric: "latency", Operator: "invalid", Threshold: 100, Severity: "warning"},
		},
	})
	if err == nil {
		t.Error("expected error for invalid operator")
	}
}

func TestNormalizeCreate_InvalidSeverity(t *testing.T) {
	_, err := NormalizeCreate(time.Now(), CreateInput{
		Name:      "test",
		CheckType: CheckTypeHTTPHealth,
		AssertionRules: []AssertionRule{
			{Metric: "latency", Operator: ">", Threshold: 100, Severity: "info"},
		},
	})
	if err == nil {
		t.Error("expected error for invalid severity")
	}
}

func TestNormalizeCreate_IntervalSchedule(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	interval := 60

	spec, err := NormalizeCreate(now, CreateInput{
		Name:         "test",
		CheckType:    CheckTypeHTTPHealth,
		ScheduleType: ScheduleTypeInterval,
		IntervalSec:  &interval,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.IntervalSec == nil || *spec.IntervalSec != 60 {
		t.Errorf("interval_sec = %v, want 60", spec.IntervalSec)
	}
	if spec.NextRunAt == nil {
		t.Error("expected NextRunAt to be set")
	}
}

func TestNormalizeCreate_MissingCronExpr(t *testing.T) {
	_, err := NormalizeCreate(time.Now(), CreateInput{
		Name:         "test",
		CheckType:    CheckTypeHTTPHealth,
		ScheduleType: ScheduleTypeCron,
	})
	if err == nil {
		t.Error("expected error for missing cron_expr")
	}
}

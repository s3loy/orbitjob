package query

import (
	"testing"

	domainjob "orbitjob/internal/core/domain/job"
)

func TestBuildScheduleSummary(t *testing.T) {
	cronExpr := "*/5 * * * *"

	// manual trigger
	if got := BuildScheduleSummary(domainjob.TriggerTypeManual, nil, ""); got != "manual" {
		t.Fatalf("expected manual summary, got %q", got)
	}

	// cron with expr and timezone
	if got := BuildScheduleSummary(domainjob.TriggerTypeCron, &cronExpr, "Asia/Shanghai"); got != "cron: */5 * * * * (Asia/Shanghai)" {
		t.Fatalf("unexpected cron summary: %q", got)
	}

	// cron with nil expr and empty timezone -> falls back to UTC
	if got := BuildScheduleSummary(domainjob.TriggerTypeCron, nil, ""); got != "cron (UTC)" {
		t.Fatalf("unexpected empty cron summary: %q", got)
	}

	// cron with empty string expr
	emptyExpr := ""
	if got := BuildScheduleSummary(domainjob.TriggerTypeCron, &emptyExpr, "UTC"); got != "cron (UTC)" {
		t.Fatalf("unexpected empty-string cron summary: %q", got)
	}

	// cron with whitespace-only expr
	blankExpr := "  "
	if got := BuildScheduleSummary(domainjob.TriggerTypeCron, &blankExpr, "UTC"); got != "cron (UTC)" {
		t.Fatalf("unexpected whitespace-only cron summary: %q", got)
	}

	// default case: unknown trigger type
	if got := BuildScheduleSummary("custom", nil, ""); got != "custom" {
		t.Fatalf("expected unknown trigger type summary, got %q", got)
	}

	// default case: unknown trigger type with whitespace (trimmed)
	if got := BuildScheduleSummary("  unknown  ", nil, ""); got != "unknown" {
		t.Fatalf("expected trimmed unknown trigger type summary, got %q", got)
	}
}

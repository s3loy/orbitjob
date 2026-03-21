package job

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeCreateJob_ManualDefaults(t *testing.T) {
	now := time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)

	out, err := NormalizeCreateJob(now, CreateJobInput{
		Name:        "demo-job",
		TriggerType: TriggerTypeManual,
		HandlerType: "http",
	})
	if err != nil {
		t.Fatalf("NormalizeCreateJob() error = %v", err)
	}

	if out.Name != "demo-job" {
		t.Fatalf("expected name=demo-job, got %q", out.Name)
	}
	if out.TenantID != DefaultTenantID {
		t.Fatalf("expected tenant_id=%q, got %q", DefaultTenantID, out.TenantID)
	}
	if out.Timezone != DefaultTimezone {
		t.Fatalf("expected timezone=%q, got %q", DefaultTimezone, out.Timezone)
	}
	if out.TimeoutSec != DefaultTimeoutSec {
		t.Fatalf("expected timeout_sec=%d, got %d", DefaultTimeoutSec, out.TimeoutSec)
	}
	if out.RetryBackoffStrategy != RetryBackoffFixed {
		t.Fatalf("expected retry_backoff_strategy=%q, got %q", RetryBackoffFixed, out.RetryBackoffStrategy)
	}
	if out.ConcurrencyPolicy != ConcurrencyAllow {
		t.Fatalf("expected concurrency_policy=%q, got %q", ConcurrencyAllow, out.ConcurrencyPolicy)
	}
	if out.MisfirePolicy != MisfireSkip {
		t.Fatalf("expected misfire_policy=%q, got %q", MisfireSkip, out.MisfirePolicy)
	}
	if out.NextRunAt != nil {
		t.Fatalf("expected next_run_at=nil for manual job, got %v", *out.NextRunAt)
	}
	if len(out.HandlerPayload) != 0 {
		t.Fatalf("expected empty handler_payload, got %#v", out.HandlerPayload)
	}
}

func TestNormalizeCreateJob_CronSetsNextRunAt(t *testing.T) {
	now := time.Date(2026, 3, 18, 0, 58, 0, 0, time.UTC)
	cronExpr := "0 9 * * *"

	out, err := NormalizeCreateJob(now, CreateJobInput{
		Name:        "daily-report",
		TriggerType: TriggerTypeCron,
		CronExpr:    &cronExpr,
		Timezone:    "Asia/Shanghai",
		HandlerType: "http",
	})
	if err != nil {
		t.Fatalf("NormalizeCreateJob() error = %v", err)
	}

	if out.NextRunAt == nil {
		t.Fatalf("expected next_run_at to be set")
	}

	want := time.Date(2026, 3, 18, 1, 0, 0, 0, time.UTC)
	if !out.NextRunAt.Equal(want) {
		t.Fatalf("expected next_run_at=%s, got %s", want.Format(time.RFC3339),
			out.NextRunAt.Format(time.RFC3339))
	}
}

func TestNormalizeCreateJob_CopiesTopLevelHandlerPayload(t *testing.T) {
	now := time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)

	original := map[string]any{
		"url": "https://example.com/hook",
	}

	out, err := NormalizeCreateJob(now, CreateJobInput{
		Name:           "demo-job",
		TriggerType:    TriggerTypeManual,
		HandlerType:    "http",
		HandlerPayload: original,
	})
	if err != nil {
		t.Fatalf("NormalizeCreateJob() error = %v", err)
	}

	original["url"] = "https://evil.example.com/hook"

	if got := out.HandlerPayload["url"]; got != "https://example.com/hook" {
		t.Fatalf("expected cloned top-level payload value to stay unchanged, got %#v", got)
	}
}

func TestNormalizeCreateJob_InvalidInput(t *testing.T) {
	cronExpr := "*/5 * * * *"

	base := CreateJobInput{
		Name:        "demo-job",
		TriggerType: TriggerTypeCron,
		CronExpr:    &cronExpr,
		Timezone:    "UTC",
		HandlerType: "http",
	}

	tests := []struct {
		name        string
		input       CreateJobInput
		wantField   string
		wantMessage string
	}{
		{
			name: "empty name",
			input: func() CreateJobInput {
				in := base
				in.Name = "   "
				return in
			}(),
			wantField:   "name",
			wantMessage: "is required",
		},
		{
			name: "name too long",
			input: func() CreateJobInput {
				in := base
				in.Name = strings.Repeat("a", 129)
				return in
			}(),
			wantField:   "name",
			wantMessage: "must be <= 128 characters",
		},
		{
			name: "empty handler type",
			input: func() CreateJobInput {
				in := base
				in.HandlerType = ""
				return in
			}(),
			wantField:   "handler_type",
			wantMessage: "is required",
		},
		{
			name: "handler type too long",
			input: func() CreateJobInput {
				in := base
				in.HandlerType = strings.Repeat("h", 33)
				return in
			}(),
			wantField:   "handler_type",
			wantMessage: "must be <= 32 characters",
		},
		{
			name: "invalid trigger type",
			input: func() CreateJobInput {
				in := base
				in.TriggerType = "delay"
				return in
			}(),
			wantField:   "trigger_type",
			wantMessage: "must be one of: cron, manual",
		},
		{
			name: "tenant too long",
			input: func() CreateJobInput {
				in := base
				in.TenantID = strings.Repeat("t", 65)
				return in
			}(),
			wantField:   "tenant_id",
			wantMessage: "must be <= 64 characters",
		},
		{
			name: "invalid timezone",
			input: func() CreateJobInput {
				in := base
				in.Timezone = "Mars/Colony"
				return in
			}(),
			wantField:   "timezone",
			wantMessage: "invalid timezone",
		},
		{
			name: "timezone too long",
			input: func() CreateJobInput {
				in := base
				in.Timezone = strings.Repeat("z", 65)
				return in
			}(),
			wantField:   "timezone",
			wantMessage: "must be <= 64 characters",
		},
		{
			name: "timeout less than one",
			input: func() CreateJobInput {
				in := base
				in.TimeoutSec = -1
				return in
			}(),
			wantField:   "timeout_sec",
			wantMessage: "must be >= 1",
		},
		{
			name: "negative retry limit",
			input: func() CreateJobInput {
				in := base
				in.RetryLimit = -1
				return in
			}(),
			wantField:   "retry_limit",
			wantMessage: "must be >= 0",
		},
		{
			name: "negative retry backoff sec",
			input: func() CreateJobInput {
				in := base
				in.RetryBackoffSec = -1
				return in
			}(),
			wantField:   "retry_backoff_sec",
			wantMessage: "must be >= 0",
		},
		{
			name: "invalid retry backoff strategy",
			input: func() CreateJobInput {
				in := base
				in.RetryBackoffStrategy = "random"
				return in
			}(),
			wantField:   "retry_backoff_strategy",
			wantMessage: "must be one of: fixed, exponential",
		},
		{
			name: "invalid concurrency policy",
			input: func() CreateJobInput {
				in := base
				in.ConcurrencyPolicy = "queue"
				return in
			}(),
			wantField:   "concurrency_policy",
			wantMessage: "must be one of: allow, forbid, replace",
		},
		{
			name: "invalid misfire policy",
			input: func() CreateJobInput {
				in := base
				in.MisfirePolicy = "delay"
				return in
			}(),
			wantField:   "misfire_policy",
			wantMessage: "must be one of: skip, fire_now, catch_up",
		},
		{
			name: "missing cron expr for cron job",
			input: CreateJobInput{
				Name:        "demo-job",
				TriggerType: TriggerTypeCron,
				HandlerType: "http",
			},
			wantField:   "cron_expr",
			wantMessage: "is required for cron jobs",
		},
		{
			name: "cron expr too long",
			input: func() CreateJobInput {
				in := base
				expr := strings.Repeat("*", 65)
				in.CronExpr = &expr
				return in
			}(),
			wantField:   "cron_expr",
			wantMessage: "must be <= 64 characters",
		},
		{
			name: "invalid cron expr",
			input: func() CreateJobInput {
				in := base
				expr := "not-a-cron"
				in.CronExpr = &expr
				return in
			}(),
			wantField:   "cron_expr",
			wantMessage: "invalid cron_expr",
		},
		{
			name: "manual job must not carry cron expr",
			input: func() CreateJobInput {
				in := base
				in.TriggerType = TriggerTypeManual
				return in
			}(),
			wantField:   "cron_expr",
			wantMessage: "must be empty for manual jobs",
		},
	}

	now := time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeCreateJob(now, tt.input)
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}

			var validationErr *ValidationError
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("expected error containing %q, got %q", tt.wantMessage, err.Error())
			}
			if !IsValidationError(err) {
				t.Fatalf("expected validation error, got %T", err)
			}
			if !AsValidationError(err, &validationErr) {
				t.Fatalf("expected error to unwrap as ValidationError")
			}
			if validationErr.Field != tt.wantField {
				t.Fatalf("expected field=%q, got %q", tt.wantField, validationErr.Field)
			}
			if validationErr.Message != tt.wantMessage {
				t.Fatalf("expected message=%q, got %q", tt.wantMessage, validationErr.Message)
			}
		})
	}
}

func TestNormalizeCreateJob_InvalidInputReturnsValidationError(t *testing.T) {
	now := time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)

	_, err := NormalizeCreateJob(now, CreateJobInput{
		TriggerType: TriggerTypeManual,
		HandlerType: "http",
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !IsValidationError(err) {
		t.Fatalf("expected validation error, got %T", err)
	}
}

func TestNormalizeCreateJob_InvalidHandlerPayload(t *testing.T) {
	now := time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)

	_, err := NormalizeCreateJob(now, CreateJobInput{
		Name:        "demo-job",
		TriggerType: TriggerTypeManual,
		HandlerType: "http",
		HandlerPayload: map[string]any{
			"bad": func() {},
		},
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !IsValidationError(err) {
		t.Fatalf("expected validation error, got %T", err)
	}

	var validationErr *ValidationError
	if !AsValidationError(err, &validationErr) {
		t.Fatalf("expected error to unwrap as ValidationError")
	}
	if validationErr.Field != "handler_payload" {
		t.Fatalf("expected field=%q, got %q", "handler_payload", validationErr.Field)
	}
}

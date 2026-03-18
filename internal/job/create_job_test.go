package job

import (
	"strings"
	"testing"
	"time"
)

func TestNewCreateJobInput(t *testing.T) {
	cronExpr := "*/5 * * * *"

	req := CreateJobRequest{
		Name:                 "demo-job",
		TenantID:             "tenant-a",
		TriggerType:          TriggerTypeCron,
		CronExpr:             &cronExpr,
		Timezone:             "Asia/Shanghai",
		HandlerType:          "http",
		HandlerPayload:       map[string]any{"url": "https://example.com/hook"},
		TimeoutSec:           120,
		RetryLimit:           3,
		RetryBackoffSec:      10,
		RetryBackoffStrategy: RetryBackoffExponential,
		ConcurrencyPolicy:    ConcurrencyForbid,
		MisfirePolicy:        MisfireFireNow,
	}

	got := NewCreateJobInput(req)

	if got.Name != req.Name {
		t.Fatalf("expected name=%q, got %q", req.Name, got.Name)
	}
	if got.TenantID != req.TenantID {
		t.Fatalf("expected tenant_id=%q, got %q", req.TenantID, got.TenantID)
	}
	if got.TriggerType != req.TriggerType {
		t.Fatalf("expected trigger_type=%q, got %q", req.TriggerType, got.TriggerType)
	}
	if got.CronExpr != req.CronExpr {
		t.Fatalf("expected cron_expr pointer to be preserved")
	}
	if got.Timezone != req.Timezone {
		t.Fatalf("expected timezone=%q, got %q", req.Timezone, got.Timezone)
	}
	if got.HandlerType != req.HandlerType {
		t.Fatalf("expected handler_type=%q, got %q", req.HandlerType, got.HandlerType)
	}
	if got.TimeoutSec != req.TimeoutSec {
		t.Fatalf("expected timeout_sec=%d, got %d", req.TimeoutSec, got.TimeoutSec)
	}
	if got.RetryLimit != req.RetryLimit {
		t.Fatalf("expected retry_limit=%d, got %d", req.RetryLimit, got.RetryLimit)
	}
	if got.RetryBackoffSec != req.RetryBackoffSec {
		t.Fatalf("expected retry_backoff_sec=%d, got %d", req.RetryBackoffSec, got.RetryBackoffSec)
	}
	if got.RetryBackoffStrategy != req.RetryBackoffStrategy {
		t.Fatalf("expected retry_backoff_strategy=%q, got %q", req.RetryBackoffStrategy,
			got.RetryBackoffStrategy)
	}
	if got.ConcurrencyPolicy != req.ConcurrencyPolicy {
		t.Fatalf("expected concurrency_policy=%q, got %q", req.ConcurrencyPolicy, got.ConcurrencyPolicy)
	}
	if got.MisfirePolicy != req.MisfirePolicy {
		t.Fatalf("expected misfire_policy=%q, got %q", req.MisfirePolicy, got.MisfirePolicy)
	}
}

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
		name    string
		input   CreateJobInput
		wantErr string
	}{
		{
			name: "empty name",
			input: func() CreateJobInput {
				in := base
				in.Name = "   "
				return in
			}(),
			wantErr: "name is required",
		},
		{
			name: "name too long",
			input: func() CreateJobInput {
				in := base
				in.Name = strings.Repeat("a", 129)
				return in
			}(),
			wantErr: "name must be <= 128 characters",
		},
		{
			name: "empty handler type",
			input: func() CreateJobInput {
				in := base
				in.HandlerType = ""
				return in
			}(),
			wantErr: "handler_type is required",
		},
		{
			name: "handler type too long",
			input: func() CreateJobInput {
				in := base
				in.HandlerType = strings.Repeat("h", 33)
				return in
			}(),
			wantErr: "handler_type must be <= 32 characters",
		},
		{
			name: "invalid trigger type",
			input: func() CreateJobInput {
				in := base
				in.TriggerType = "delay"
				return in
			}(),
			wantErr: "trigger_type must be one of",
		},
		{
			name: "tenant too long",
			input: func() CreateJobInput {
				in := base
				in.TenantID = strings.Repeat("t", 65)
				return in
			}(),
			wantErr: "tenant_id must be <= 64 characters",
		},
		{
			name: "invalid timezone",
			input: func() CreateJobInput {
				in := base
				in.Timezone = "Mars/Colony"
				return in
			}(),
			wantErr: "invalid timezone",
		},
		{
			name: "timezone too long",
			input: func() CreateJobInput {
				in := base
				in.Timezone = strings.Repeat("z", 65)
				return in
			}(),
			wantErr: "timezone must be <= 64 characters",
		},
		{
			name: "timeout less than one",
			input: func() CreateJobInput {
				in := base
				in.TimeoutSec = -1
				return in
			}(),
			wantErr: "timeout_sec must be >= 1",
		},
		{
			name: "negative retry limit",
			input: func() CreateJobInput {
				in := base
				in.RetryLimit = -1
				return in
			}(),
			wantErr: "retry_limit must be >= 0",
		},
		{
			name: "negative retry backoff sec",
			input: func() CreateJobInput {
				in := base
				in.RetryBackoffSec = -1
				return in
			}(),
			wantErr: "retry_backoff_sec must be >= 0",
		},
		{
			name: "invalid retry backoff strategy",
			input: func() CreateJobInput {
				in := base
				in.RetryBackoffStrategy = "random"
				return in
			}(),
			wantErr: "retry_backoff_strategy must be one of: fixed, exponential",
		},
		{
			name: "invalid concurrency policy",
			input: func() CreateJobInput {
				in := base
				in.ConcurrencyPolicy = "queue"
				return in
			}(),
			wantErr: "concurrency_policy must be one of: allow, forbid, replace",
		},
		{
			name: "invalid misfire policy",
			input: func() CreateJobInput {
				in := base
				in.MisfirePolicy = "delay"
				return in
			}(),
			wantErr: "misfire_policy must be one of: skip, fire_now, catch_up",
		},
		{
			name: "missing cron expr for cron job",
			input: CreateJobInput{
				Name:        "demo-job",
				TriggerType: TriggerTypeCron,
				HandlerType: "http",
			},
			wantErr: "cron_expr is required for cron jobs",
		},
		{
			name: "cron expr too long",
			input: func() CreateJobInput {
				in := base
				expr := strings.Repeat("*", 65)
				in.CronExpr = &expr
				return in
			}(),
			wantErr: "cron_expr must be <= 64 characters",
		},
		{
			name: "invalid cron expr",
			input: func() CreateJobInput {
				in := base
				expr := "not-a-cron"
				in.CronExpr = &expr
				return in
			}(),
			wantErr: "invalid cron_expr",
		},
		{
			name: "manual job must not carry cron expr",
			input: func() CreateJobInput {
				in := base
				in.TriggerType = TriggerTypeManual
				return in
			}(),
			wantErr: "cron_expr must be empty for manual jobs",
		},
	}

	now := time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeCreateJob(now, tt.input)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

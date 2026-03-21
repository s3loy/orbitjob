package httpapi

import (
	"testing"

	"orbitjob/internal/job"
)

func TestCreateJobRequest_ToCreateJobInput(t *testing.T) {
	cronExpr := "*/5 * * * *"

	req := CreateJobRequest{
		Name:                 "demo-job",
		TenantID:             "tenant-a",
		TriggerType:          job.TriggerTypeCron,
		CronExpr:             &cronExpr,
		Timezone:             "Asia/Shanghai",
		HandlerType:          "http",
		HandlerPayload:       map[string]any{"url": "https://example.com/hook"},
		TimeoutSec:           120,
		RetryLimit:           3,
		RetryBackoffSec:      10,
		RetryBackoffStrategy: job.RetryBackoffExponential,
		ConcurrencyPolicy:    job.ConcurrencyForbid,
		MisfirePolicy:        job.MisfireFireNow,
	}

	got := req.ToCreateJobInput()

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
		t.Fatalf("expected retry_backoff_strategy=%q, got %q", req.RetryBackoffStrategy, got.RetryBackoffStrategy)
	}
	if got.ConcurrencyPolicy != req.ConcurrencyPolicy {
		t.Fatalf("expected concurrency_policy=%q, got %q", req.ConcurrencyPolicy, got.ConcurrencyPolicy)
	}
	if got.MisfirePolicy != req.MisfirePolicy {
		t.Fatalf("expected misfire_policy=%q, got %q", req.MisfirePolicy, got.MisfirePolicy)
	}
}

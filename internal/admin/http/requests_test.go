package http

import (
	"testing"

	query "orbitjob/internal/admin/app/job/query"
)

func TestCreateJobRequest_ToCreateInput(t *testing.T) {
	cronExpr := "*/5 * * * *"

	req := CreateJobRequest{
		Name:                 "demo-job",
		TenantID:             "tenant-a",
		TriggerType:          "cron",
		CronExpr:             &cronExpr,
		Timezone:             "Asia/Shanghai",
		HandlerType:          "http",
		HandlerPayload:       map[string]any{"url": "https://example.com/hook"},
		TimeoutSec:           120,
		RetryLimit:           3,
		RetryBackoffSec:      10,
		RetryBackoffStrategy: "exponential",
		ConcurrencyPolicy:    "forbid",
		MisfirePolicy:        "fire_now",
	}

	got := req.ToCreateInput()

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

func TestListJobsRequest_ToListInput(t *testing.T) {
	req := ListJobsRequest{
		TenantID: "tenant-a",
		Status:   query.StatusActive,
		Limit:    20,
		Offset:   40,
	}

	got := req.ToListInput()

	if got.TenantID != req.TenantID {
		t.Fatalf("expected tenant_id=%q, got %q", req.TenantID, got.TenantID)
	}
	if got.Status != req.Status {
		t.Fatalf("expected status=%q, got %q", req.Status, got.Status)
	}
	if got.Limit != req.Limit {
		t.Fatalf("expected limit=%d, got %d", req.Limit, got.Limit)
	}
	if got.Offset != req.Offset {
		t.Fatalf("expected offset=%d, got %d", req.Offset, got.Offset)
	}
}

func TestGetJobRequest_ToGetInput(t *testing.T) {
	req := GetJobRequest{
		ID:       42,
		TenantID: "tenant-a",
	}

	got := req.ToGetInput()

	if got.ID != req.ID {
		t.Fatalf("expected id=%d, got %d", req.ID, got.ID)
	}
	if got.TenantID != req.TenantID {
		t.Fatalf("expected tenant_id=%q, got %q", req.TenantID, got.TenantID)
	}
}

func TestUpdateJobRequest_ToUpdateInput(t *testing.T) {
	cronExpr := "*/15 * * * *"
	name := "nightly-report"
	timeoutSec := 120

	req := UpdateJobRequest{
		ID:         42,
		TenantID:   "tenant-a",
		Version:    7,
		Name:       &name,
		CronExpr:   &cronExpr,
		TimeoutSec: &timeoutSec,
	}

	current := query.GetItem{
		ID:                   42,
		TenantID:             "tenant-a",
		Name:                 "old-name",
		TriggerType:          "manual",
		Timezone:             "UTC",
		HandlerType:          "worker",
		HandlerPayload:       map[string]any{"queue": "jobs"},
		TimeoutSec:           60,
		RetryLimit:           3,
		RetryBackoffSec:      10,
		RetryBackoffStrategy: "fixed",
		ConcurrencyPolicy:    "allow",
		MisfirePolicy:        "skip",
	}

	got := req.ToUpdateInput(current, "control-plane-user")

	if got.ID != req.ID {
		t.Fatalf("expected id=%d, got %d", req.ID, got.ID)
	}
	if got.TenantID != req.TenantID {
		t.Fatalf("expected tenant_id=%q, got %q", req.TenantID, got.TenantID)
	}
	if got.ChangedBy != "control-plane-user" {
		t.Fatalf("expected changed_by=%q, got %q", "control-plane-user", got.ChangedBy)
	}
	if got.Version != req.Version {
		t.Fatalf("expected version=%d, got %d", req.Version, got.Version)
	}
	if got.Name != name {
		t.Fatalf("expected name=%q, got %q", name, got.Name)
	}
	if got.TriggerType != current.TriggerType {
		t.Fatalf("expected trigger_type=%q, got %q", current.TriggerType, got.TriggerType)
	}
	if got.CronExpr == nil || *got.CronExpr != cronExpr {
		t.Fatalf("expected cron_expr=%q, got %+v", cronExpr, got.CronExpr)
	}
	if got.TimeoutSec != timeoutSec {
		t.Fatalf("expected timeout_sec=%d, got %d", timeoutSec, got.TimeoutSec)
	}
	if got.HandlerType != current.HandlerType {
		t.Fatalf("expected handler_type=%q, got %q", current.HandlerType, got.HandlerType)
	}
	if got.HandlerPayload["queue"] != "jobs" {
		t.Fatalf("expected handler payload queue to be preserved")
	}
	if got.ConcurrencyPolicy != current.ConcurrencyPolicy {
		t.Fatalf("expected concurrency_policy=%q, got %q", current.ConcurrencyPolicy, got.ConcurrencyPolicy)
	}
}

func TestUpdateJobRequest_ToUpdateInputSwitchingToManualClearsCron(t *testing.T) {
	triggerType := "manual"
	currentCron := "*/15 * * * *"

	req := UpdateJobRequest{
		ID:          42,
		TenantID:    "tenant-a",
		Version:     7,
		TriggerType: &triggerType,
	}

	current := query.GetItem{
		ID:          42,
		TenantID:    "tenant-a",
		Name:        "nightly-report",
		TriggerType: "cron",
		CronExpr:    &currentCron,
		Timezone:    "Asia/Shanghai",
		HandlerType: "http",
	}

	got := req.ToUpdateInput(current, "control-plane-user")

	if got.TriggerType != triggerType {
		t.Fatalf("expected trigger_type=%q, got %q", triggerType, got.TriggerType)
	}
	if got.CronExpr != nil {
		t.Fatalf("expected cron_expr to be cleared when switching to manual")
	}
}

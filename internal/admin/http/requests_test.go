package http

import (
	"testing"

	query "orbitjob/internal/admin/app/job/query"
)

func TestCreateJobRequest_ToCreateInput(t *testing.T) {
	cronExpr := "*/5 * * * *"
	partitionKey := "tenant-a:video"

	req := CreateJobRequest{
		Name:                 "demo-job",
		TenantID:             "tenant-a",
		Priority:             8,
		PartitionKey:         &partitionKey,
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
	if got.Priority != req.Priority {
		t.Fatalf("expected priority=%d, got %d", req.Priority, got.Priority)
	}
	if got.PartitionKey != req.PartitionKey {
		t.Fatalf("expected partition_key pointer to be preserved")
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
		ID:       42,
		TenantID: "tenant-a",
		Version:  7,
		Name:     &name,
		Priority: func() *int {
			value := 11
			return &value
		}(),
		PartitionKey: func() *string {
			value := "shard-blue"
			return &value
		}(),
		CronExpr:   &cronExpr,
		TimeoutSec: &timeoutSec,
	}

	current := query.GetItem{
		ID:                   42,
		TenantID:             "tenant-a",
		Name:                 "old-name",
		Priority:             5,
		TriggerType:          "manual",
		Timezone:             "UTC",
		HandlerType:          "http",
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
	if got.Priority != 11 {
		t.Fatalf("expected priority=%d, got %d", 11, got.Priority)
	}
	if got.PartitionKey == nil || *got.PartitionKey != "shard-blue" {
		t.Fatalf("expected partition_key=%q, got %+v", "shard-blue", got.PartitionKey)
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
		Priority:    4,
		TriggerType: "cron",
		CronExpr:    &currentCron,
		Timezone:    "Asia/Shanghai",
		HandlerType: "http",
		PartitionKey: func() *string {
			value := "shard-green"
			return &value
		}(),
	}

	got := req.ToUpdateInput(current, "control-plane-user")

	if got.TriggerType != triggerType {
		t.Fatalf("expected trigger_type=%q, got %q", triggerType, got.TriggerType)
	}
	if got.CronExpr != nil {
		t.Fatalf("expected cron_expr to be cleared when switching to manual")
	}
}

func TestMapValueOrDefault_ProvidesHandlerPayload(t *testing.T) {
	// When HandlerPayload is provided in the request, it should be cloned (non-nil branch).
	handlerPayload := map[string]any{"custom": "value"}
	req := UpdateJobRequest{
		ID:             42,
		Version:        1,
		HandlerPayload: handlerPayload,
	}

	current := query.GetItem{
		ID:             42,
		Name:           "test-job",
		HandlerPayload: map[string]any{"old": "data"},
	}

	got := req.ToUpdateInput(current, "user")

	if got.HandlerPayload["custom"] != "value" {
		t.Fatalf("expected request HandlerPayload to be used, got %+v", got.HandlerPayload)
	}
	if len(got.HandlerPayload) != 1 {
		t.Fatalf("expected only the request's payload keys, got %d", len(got.HandlerPayload))
	}
	// Verify it's a clone, not the same reference (defensive copy)
	handlerPayload["custom"] = "mutated"
	if got.HandlerPayload["custom"] != "value" {
		t.Fatalf("expected cloned map to not alias original, got %+v", got.HandlerPayload)
	}
}

func TestMapValueOrDefault_NilFallbackClone(t *testing.T) {
	// When value is nil, fallback should be cloned.
	fallback := map[string]any{"a": "b"}
	req := UpdateJobRequest{
		ID:      42,
		Version: 1,
	}

	current := query.GetItem{
		ID:             42,
		Name:           "test-job",
		HandlerPayload: fallback,
	}

	got := req.ToUpdateInput(current, "user")

	if got.HandlerPayload["a"] != "b" {
		t.Fatalf("expected fallback payload, got %+v", got.HandlerPayload)
	}
	fallback["a"] = "mutated"
	if got.HandlerPayload["a"] != "b" {
		t.Fatalf("expected cloned fallback to not alias original")
	}
}

func TestCloneMap_Empty(t *testing.T) {
	got := cloneMap(nil)
	if got == nil {
		t.Fatal("expected non-nil empty map from cloneMap(nil)")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(got))
	}
}

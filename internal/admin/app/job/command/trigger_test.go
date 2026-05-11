package command

import (
	"context"
	"errors"
	"testing"

	query "orbitjob/internal/admin/app/job/query"
	domaininstance "orbitjob/internal/core/domain/instance"
	domainjob "orbitjob/internal/core/domain/job"
)

type stubJobReader struct {
	item query.GetItem
	err  error
}

func (r *stubJobReader) Get(_ context.Context, _ query.GetInput) (query.GetItem, error) {
	return r.item, r.err
}

type stubInstanceCreator struct {
	out domaininstance.Snapshot
	err error
}

func (r *stubInstanceCreator) Create(_ context.Context, _ domaininstance.CreateSpec) (domaininstance.Snapshot, error) {
	return r.out, r.err
}

type captureInstanceCreator struct {
	captured *domaininstance.CreateSpec
	out      domaininstance.Snapshot
	err      error
}

func (r *captureInstanceCreator) Create(_ context.Context, spec domaininstance.CreateSpec) (domaininstance.Snapshot, error) {
	r.captured = &spec
	return r.out, r.err
}

func TestNewTriggerJobUseCase(t *testing.T) {
	uc := NewTriggerJobUseCase(&stubJobReader{}, &stubInstanceCreator{})
	if uc == nil {
		t.Fatal("expected use case to be initialized")
	}
}

func TestTriggerJobUseCase_Trigger(t *testing.T) {
	partitionKey := "east"
	reader := &stubJobReader{
		item: query.GetItem{
			ID:             1,
			Name:           "daily-report",
			TenantID:       "default",
			Status:         domainjob.StatusActive,
			Priority:       10,
			PartitionKey:   &partitionKey,
			HandlerType:    "exec",
			HandlerPayload: map[string]any{"cmd": "echo"},
			RetryLimit:     3,
		},
	}
	creator := &stubInstanceCreator{
		out: domaininstance.Snapshot{
			RunID:         "run-trigger-1",
			JobID:         1,
			TenantID:      "default",
			Status:        domaininstance.StatusPending,
			TriggerSource: domaininstance.TriggerSourceManual,
		},
	}
	uc := NewTriggerJobUseCase(reader, creator)

	out, err := uc.Trigger(context.Background(), TriggerInput{JobID: 1, TenantID: "default"})
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	if out.RunID != "run-trigger-1" {
		t.Fatalf("expected RunID=%q, got %q", "run-trigger-1", out.RunID)
	}
	if out.Status != domaininstance.StatusPending {
		t.Fatalf("expected Status=%q, got %q", domaininstance.StatusPending, out.Status)
	}
}

func TestTriggerJobUseCase_Trigger_JobNotFound(t *testing.T) {
	reader := &stubJobReader{err: errors.New("not found")}
	creator := &stubInstanceCreator{}
	uc := NewTriggerJobUseCase(reader, creator)

	_, err := uc.Trigger(context.Background(), TriggerInput{JobID: 999, TenantID: "default"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTriggerJobUseCase_Trigger_JobNotActive(t *testing.T) {
	reader := &stubJobReader{
		item: query.GetItem{ID: 1, Status: domainjob.StatusPaused},
	}
	creator := &stubInstanceCreator{}
	uc := NewTriggerJobUseCase(reader, creator)

	_, err := uc.Trigger(context.Background(), TriggerInput{JobID: 1, TenantID: "default"})
	if err == nil {
		t.Fatal("expected error for non-active job, got nil")
	}
}

func TestTriggerJobUseCase_Trigger_InstanceCreateError(t *testing.T) {
	reader := &stubJobReader{
		item: query.GetItem{
			ID:       1,
			Status:   domainjob.StatusActive,
			TenantID: "default",
		},
	}
	creator := &stubInstanceCreator{err: errors.New("duplicate idempotency key")}
	uc := NewTriggerJobUseCase(reader, creator)

	_, err := uc.Trigger(context.Background(), TriggerInput{JobID: 1, TenantID: "default"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTriggerJobUseCase_Trigger_NormalizeCreateError(t *testing.T) {
	// TenantID > 64 characters triggers a NormalizeCreate validation error.
	longTenant := "this-tenant-id-is-way-too-long-and-exceeds-the-sixty-four-character-limit-imposed-by-domain-validation"
	reader := &stubJobReader{
		item: query.GetItem{
			ID:       1,
			Status:   domainjob.StatusActive,
			TenantID: longTenant,
		},
	}
	creator := &stubInstanceCreator{}
	uc := NewTriggerJobUseCase(reader, creator)

	_, err := uc.Trigger(context.Background(), TriggerInput{JobID: 1, TenantID: longTenant})
	if err == nil {
		t.Fatal("expected normalize create error, got nil")
	}
}

func TestTriggerJobUseCase_Trigger_JobIDZero(t *testing.T) {
	// JobID=0 triggers NormalizeCreate validation error (job_id must be >= 1).
	reader := &stubJobReader{
		item: query.GetItem{
			ID:       0,
			Status:   domainjob.StatusActive,
			TenantID: "default",
		},
	}
	creator := &stubInstanceCreator{}
	uc := NewTriggerJobUseCase(reader, creator)

	_, err := uc.Trigger(context.Background(), TriggerInput{JobID: 0, TenantID: "default"})
	if err == nil {
		t.Fatal("expected normalize create error for JobID=0, got nil")
	}
}

func TestIdempotencyKeyPtr(t *testing.T) {
	if p := idempotencyKeyPtr(""); p != nil {
		t.Fatalf("expected nil for empty string, got %v", *p)
	}
	if p := idempotencyKeyPtr("key-1"); p == nil || *p != "key-1" {
		t.Fatalf("expected pointer to key-1, got %v", p)
	}
}

func TestTriggerJobUseCase_ManualTriggerSource(t *testing.T) {
	reader := &stubJobReader{
		item: query.GetItem{
			ID:             1,
			Name:           "test",
			TenantID:       "default",
			Status:         domainjob.StatusActive,
			HandlerType:    "exec",
			HandlerPayload: map[string]any{"cmd": "true"},
		},
	}
	captor := &captureInstanceCreator{
		out: domaininstance.Snapshot{RunID: "run-1", Status: domaininstance.StatusPending},
	}
	uc := NewTriggerJobUseCase(reader, captor)

	_, err := uc.Trigger(context.Background(), TriggerInput{JobID: 1, TenantID: "default"})
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	if captor.captured == nil {
		t.Fatal("expected Create to be called")
	}
	if captor.captured.TriggerSource != domaininstance.TriggerSourceManual {
		t.Fatalf("expected TriggerSource=%q, got %q", domaininstance.TriggerSourceManual, captor.captured.TriggerSource)
	}
	if captor.captured.MaxAttempt != 1 { // stub RetryLimit is 0, so 0 + 1 = 1
		t.Fatalf("expected MaxAttempt=1, got %d", captor.captured.MaxAttempt)
	}
	if captor.captured.ScheduledAt.IsZero() {
		t.Fatal("expected ScheduledAt to be set")
	}
}

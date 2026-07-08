package command

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus/testutil"

	query "orbitjob/internal/admin/app/job/query"
	"orbitjob/internal/admin/http/middleware"
	domaininstance "orbitjob/internal/core/domain/instance"
	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/platform/metrics"
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

type stubInstanceReaderByIdempotency struct {
	out domaininstance.Snapshot
	err error
}

func (r *stubInstanceReaderByIdempotency) GetByIdempotencyKey(_ context.Context, _, _, _ string) (domaininstance.Snapshot, error) {
	return r.out, r.err
}

type idempotencyResult struct {
	out domaininstance.Snapshot
	err error
}

type sequentialIdempotencyReader struct {
	sequence []idempotencyResult
	index    int
}

func (r *sequentialIdempotencyReader) GetByIdempotencyKey(_ context.Context, _, _, _ string) (domaininstance.Snapshot, error) {
	if r.index >= len(r.sequence) {
		return domaininstance.Snapshot{}, errors.New("unexpected idempotency lookup call")
	}
	res := r.sequence[r.index]
	r.index++
	return res.out, res.err
}

func TestNewTriggerJobUseCase(t *testing.T) {
	uc := NewTriggerJobUseCase(&stubJobReader{}, &stubInstanceCreator{}, &stubInstanceReaderByIdempotency{})
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
	uc := NewTriggerJobUseCase(reader, creator, &stubInstanceReaderByIdempotency{err: errors.New("not found")})

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
	uc := NewTriggerJobUseCase(reader, creator, &stubInstanceReaderByIdempotency{err: errors.New("not found")})

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
	uc := NewTriggerJobUseCase(reader, creator, &stubInstanceReaderByIdempotency{err: errors.New("not found")})

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
	uc := NewTriggerJobUseCase(reader, creator, &stubInstanceReaderByIdempotency{err: errors.New("not found")})

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
	uc := NewTriggerJobUseCase(reader, creator, &stubInstanceReaderByIdempotency{err: errors.New("not found")})

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
	uc := NewTriggerJobUseCase(reader, creator, &stubInstanceReaderByIdempotency{err: errors.New("not found")})

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
	uc := NewTriggerJobUseCase(reader, captor, &stubInstanceReaderByIdempotency{err: errors.New("not found")})

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

func TestTriggerJobUseCase_Trigger_IdempotentDuplicate(t *testing.T) {
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
	existing := domaininstance.Snapshot{
		RunID:            "run-existing",
		JobID:            1,
		TenantID:         "default",
		Status:           domaininstance.StatusPending,
		TriggerSource:    domaininstance.TriggerSourceManual,
		IdempotencyKey:   strPtr("idem-1"),
		IdempotencyScope: domaininstance.DefaultIdempotencyScope,
	}
	captor := &captureInstanceCreator{}
	idempotency := &stubInstanceReaderByIdempotency{out: existing}
	uc := NewTriggerJobUseCase(reader, captor, idempotency)

	ctx := middleware.WithIdempotencyKey(context.Background(), "idem-1")
	out, err := uc.Trigger(ctx, TriggerInput{JobID: 1, TenantID: "default"})
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	if captor.captured != nil {
		t.Fatal("expected Create not to be called for duplicate idempotency key")
	}
	if out.RunID != "run-existing" {
		t.Fatalf("expected RunID=%q, got %q", "run-existing", out.RunID)
	}
	if out.Created {
		t.Fatal("expected Created=false for idempotent duplicate")
	}
}

func TestTriggerJobUseCase_Trigger_CreatedFlag(t *testing.T) {
	reader := &stubJobReader{
		item: query.GetItem{
			ID:       1,
			Status:   domainjob.StatusActive,
			TenantID: "default",
		},
	}
	creator := &stubInstanceCreator{
		out: domaininstance.Snapshot{
			RunID:  "run-new",
			JobID:  1,
			Status: domaininstance.StatusPending,
		},
	}
	uc := NewTriggerJobUseCase(reader, creator, &stubInstanceReaderByIdempotency{err: errors.New("not found")})

	out, err := uc.Trigger(context.Background(), TriggerInput{JobID: 1, TenantID: "default"})
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	if out.RunID != "run-new" {
		t.Fatalf("expected RunID=%q, got %q", "run-new", out.RunID)
	}
	if !out.Created {
		t.Fatal("expected Created=true for newly created instance")
	}
}

func strPtr(s string) *string {
	return &s
}

func TestTriggerJobUseCase_Trigger_LatencyMetricObserved(t *testing.T) {
	metrics.TriggerLatency.Reset()

	reader := &stubJobReader{
		item: query.GetItem{
			ID:       1,
			Status:   domainjob.StatusActive,
			TenantID: "default",
		},
	}
	creator := &stubInstanceCreator{
		out: domaininstance.Snapshot{
			RunID:    "run-metric",
			JobID:    1,
			TenantID: "default",
			Status:   domaininstance.StatusPending,
		},
	}
	uc := NewTriggerJobUseCase(reader, creator, &stubInstanceReaderByIdempotency{err: errors.New("not found")})

	_, err := uc.Trigger(context.Background(), TriggerInput{JobID: 1, TenantID: "default"})
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}

	count := testutil.CollectAndCount(metrics.TriggerLatency, "orbitjob_trigger_latency_seconds")
	if count != 1 {
		t.Fatalf("expected 1 latency observation, got %d", count)
	}
}

func TestTriggerJobUseCase_Trigger_IdempotentRaceOnCreate(t *testing.T) {
	metrics.TriggerLatency.Reset()
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
	existing := domaininstance.Snapshot{
		RunID:            "run-existing-race",
		JobID:            1,
		TenantID:         "default",
		Status:           domaininstance.StatusPending,
		TriggerSource:    domaininstance.TriggerSourceManual,
		IdempotencyKey:   strPtr("idem-race"),
		IdempotencyScope: domaininstance.DefaultIdempotencyScope,
	}
	creator := &captureInstanceCreator{err: &pq.Error{Code: "23505"}}
	idempotency := &sequentialIdempotencyReader{
		sequence: []idempotencyResult{
			{err: sql.ErrNoRows},
			{out: existing},
		},
	}
	uc := NewTriggerJobUseCase(reader, creator, idempotency)

	ctx := middleware.WithIdempotencyKey(context.Background(), "idem-race")
	out, err := uc.Trigger(ctx, TriggerInput{JobID: 1, TenantID: "default"})
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	if creator.captured == nil {
		t.Fatal("expected Create to be called")
	}
	if out.RunID != "run-existing-race" {
		t.Fatalf("expected RunID=%q, got %q", "run-existing-race", out.RunID)
	}
	if out.Created {
		t.Fatal("expected Created=false for recovered idempotent duplicate")
	}
	count := testutil.CollectAndCount(metrics.TriggerLatency, "orbitjob_trigger_latency_seconds")
	if count != 0 {
		t.Fatalf("expected 0 latency observations on retry path, got %d", count)
	}
}

func TestTriggerJobUseCase_Trigger_UniqueViolationRequeryNoRows(t *testing.T) {
	reader := &stubJobReader{
		item: query.GetItem{
			ID:       1,
			Status:   domainjob.StatusActive,
			TenantID: "default",
		},
	}
	createErr := &pq.Error{Code: "23505"}
	creator := &captureInstanceCreator{err: createErr}
	idempotency := &sequentialIdempotencyReader{
		sequence: []idempotencyResult{
			{err: sql.ErrNoRows},
			{err: sql.ErrNoRows},
		},
	}
	uc := NewTriggerJobUseCase(reader, creator, idempotency)

	ctx := middleware.WithIdempotencyKey(context.Background(), "idem-lost")
	_, err := uc.Trigger(ctx, TriggerInput{JobID: 1, TenantID: "default"})
	if err == nil {
		t.Fatal("expected error when re-query after unique violation returns no rows")
	}
}

func TestTriggerJobUseCase_Trigger_UniqueViolationRequeryError(t *testing.T) {
	reader := &stubJobReader{
		item: query.GetItem{
			ID:       1,
			Status:   domainjob.StatusActive,
			TenantID: "default",
		},
	}
	createErr := &pq.Error{Code: "23505"}
	creator := &captureInstanceCreator{err: createErr}
	idempotency := &sequentialIdempotencyReader{
		sequence: []idempotencyResult{
			{err: sql.ErrNoRows},
			{err: errors.New("lookup failed")},
		},
	}
	uc := NewTriggerJobUseCase(reader, creator, idempotency)

	ctx := middleware.WithIdempotencyKey(context.Background(), "idem-lookup-error")
	_, err := uc.Trigger(ctx, TriggerInput{JobID: 1, TenantID: "default"})
	if err == nil {
		t.Fatal("expected error when re-query after unique violation fails")
	}
}

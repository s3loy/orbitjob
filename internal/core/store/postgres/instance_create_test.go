package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	domaininstance "orbitjob/internal/core/domain/instance"
	tenant "orbitjob/internal/core/domain/tenant"
)

var instanceColumnsAll = []string{
	"id", "run_id", "tenant_id", "job_id", "trigger_source",
	"status", "priority", "effective_priority", "partition_key",
	"idempotency_key", "idempotency_scope", "routing_key", "worker_id",
	"attempt", "max_attempt", "scheduled_at", "started_at",
	"finished_at", "lease_expires_at", "dispatched_at", "retry_at",
	"result_code", "error_msg", "trace_id", "created_at", "updated_at",
	"version",
}

func TestInstanceRepository_CreateUnit_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewInstanceRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	spec := domaininstance.CreateSpec{
		TenantID:      "tenant-a",
		JobID:         42,
		TriggerSource: domaininstance.TriggerSourceSchedule,
		ScheduledAt:   now,
		Priority:      5,
		MaxAttempt:    3,
	}

	mock.ExpectQuery("INSERT INTO job_instances").
		WithArgs("tenant-a", int64(42), "schedule", now, 5, nil, nil, "", nil, 3, nil).
		WillReturnRows(sqlmock.NewRows(instanceColumnsAll).AddRow(
			int64(1), "run-1", "tenant-a", int64(42), "schedule",
			"pending", 5, 5, nil, nil, // effective_priority, partition_key, idempotency_key
			"job_instance_create", nil, nil, // idempotency_scope, routing_key, worker_id
			1, 3, // attempt, max_attempt
			now, nil, nil, // scheduled_at, started_at, finished_at
			nil, nil, nil, // lease_expires_at, dispatched_at, retry_at
			nil, nil, nil, // result_code, error_msg, trace_id
			now, now, // created_at, updated_at
			1, // version
		))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeSystem, "system", tenant.EventTypeInstanceCreated, tenant.ResourceTypeInstance, "run-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	out, err := repo.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if out.ID != 1 {
		t.Fatalf("expected id=1, got %d", out.ID)
	}
	if out.RunID != "run-1" {
		t.Fatalf("expected run_id=run-1, got %q", out.RunID)
	}
	if out.Status != domaininstance.StatusPending {
		t.Fatalf("expected status=pending, got %q", out.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestInstanceRepository_CreateUnit_InsertError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewInstanceRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	spec := domaininstance.CreateSpec{
		TenantID:      "tenant-a",
		JobID:         42,
		TriggerSource: domaininstance.TriggerSourceSchedule,
		ScheduledAt:   now,
		Priority:      5,
		MaxAttempt:    3,
	}

	mock.ExpectQuery("INSERT INTO job_instances").
		WithArgs("tenant-a", int64(42), "schedule", now, 5, nil, nil, "", nil, 3, nil).
		WillReturnError(errors.New("insert boom"))

	_, err = repo.Create(context.Background(), spec)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !containsStr(err.Error(), "insert job instance") {
		t.Fatalf("expected insert job instance error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestInstanceRepository_CreateUnit_AuditInsertError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewInstanceRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	spec := domaininstance.CreateSpec{
		TenantID:      "tenant-a",
		JobID:         42,
		TriggerSource: domaininstance.TriggerSourceSchedule,
		ScheduledAt:   now,
		Priority:      5,
		MaxAttempt:    3,
	}

	mock.ExpectQuery("INSERT INTO job_instances").
		WithArgs("tenant-a", int64(42), "schedule", now, 5, nil, nil, "", nil, 3, nil).
		WillReturnRows(sqlmock.NewRows(instanceColumnsAll).AddRow(
			int64(1), "run-1", "tenant-a", int64(42), "schedule",
			"pending", 5, 5, nil, nil,
			"job_instance_create", nil, nil,
			1, 3,
			now, nil, nil,
			nil, nil, nil,
			nil, nil, nil,
			now, now,
			1,
		))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeSystem, "system", tenant.EventTypeInstanceCreated, tenant.ResourceTypeInstance, "run-1", sqlmock.AnyArg()).
		WillReturnError(errors.New("audit boom"))

	_, err = repo.Create(context.Background(), spec)
	if err == nil || !containsStr(err.Error(), "insert audit event") {
		t.Fatalf("expected insert audit event error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstr(s, substr)
}

func searchSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

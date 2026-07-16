package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	domainjob "orbitjob/internal/core/domain/job"
	tenant "orbitjob/internal/core/domain/tenant"
)

func TestJobRepository_CreateUnit_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	nextRun := now.Add(5 * time.Minute)

	spec := domainjob.CreateSpec{
		Name:                 "test-job",
		TenantID:             "tenant-a",
		Priority:             5,
		TriggerType:          domainjob.TriggerTypeCron,
		CronExpr:             strPtr("*/5 * * * *"),
		Timezone:             "UTC",
		HandlerType:          "http",
		HandlerPayload:       map[string]any{"url": "https://example.com"},
		TimeoutSec:           60,
		RetryLimit:           3,
		RetryBackoffSec:      10,
		RetryBackoffStrategy: domainjob.RetryBackoffExponential,
		ConcurrencyPolicy:    domainjob.ConcurrencyAllow,
		MisfirePolicy:        domainjob.MisfireSkip,
		NextRunAt:            &nextRun,
	}

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").WithArgs("tenant-a").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("INSERT INTO jobs").
		WithArgs(
			"test-job", "tenant-a", 5, nil, "cron", "*/5 * * * *", "UTC", "http",
			sqlmock.AnyArg(), // handler_payload jsonb
			60, 3, 10, "exponential", "allow", "skip", &nextRun,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "tenant_id", "status", "version", "next_run_at", "created_at", "updated_at",
		}).AddRow(int64(1), "test-job", "tenant-a", "active", 1, nextRun, now, now))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeSystem, "system", tenant.EventTypeJobCreated, tenant.ResourceTypeJob, "1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	out, err := repo.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if out.ID != 1 {
		t.Fatalf("expected id=1, got %d", out.ID)
	}
	if out.Name != "test-job" {
		t.Fatalf("expected name=test-job, got %q", out.Name)
	}
	if out.Status != "active" {
		t.Fatalf("expected status=active, got %q", out.Status)
	}
	if out.Version != 1 {
		t.Fatalf("expected version=1, got %d", out.Version)
	}
	if out.NextRunAt == nil || !out.NextRunAt.Equal(nextRun) {
		t.Fatalf("expected next_run_at to be set")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_CreateUnit_InsertError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	nextRun := now.Add(5 * time.Minute)

	spec := domainjob.CreateSpec{
		Name:                 "test-job",
		TenantID:             "tenant-a",
		Priority:             5,
		TriggerType:          domainjob.TriggerTypeCron,
		CronExpr:             strPtr("*/5 * * * *"),
		Timezone:             "UTC",
		HandlerType:          "http",
		HandlerPayload:       map[string]any{},
		TimeoutSec:           60,
		RetryLimit:           3,
		RetryBackoffSec:      10,
		RetryBackoffStrategy: domainjob.RetryBackoffFixed,
		ConcurrencyPolicy:    domainjob.ConcurrencyAllow,
		MisfirePolicy:        domainjob.MisfireSkip,
		NextRunAt:            &nextRun,
	}

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").WithArgs("tenant-a").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("INSERT INTO jobs").
		WithArgs(
			"test-job", "tenant-a", 5, nil, "cron", "*/5 * * * *", "UTC", "http",
			sqlmock.AnyArg(), 60, 3, 10, "fixed", "allow", "skip", &nextRun,
		).
		WillReturnError(errors.New("insert boom"))
	mock.ExpectRollback()

	_, err = repo.Create(context.Background(), spec)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "insert job") {
		t.Fatalf("expected insert job error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_CreateUnit_MarshalPayloadError(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)

	// Channel values are not JSON-serializable
	spec := domainjob.CreateSpec{
		Name:           "test-job",
		TenantID:       "tenant-a",
		TriggerType:    domainjob.TriggerTypeManual,
		HandlerType:    "http",
		HandlerPayload: map[string]any{"ch": make(chan int)},
	}

	_, err = repo.Create(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "marshal handler_payload") {
		t.Fatalf("expected marshal handler_payload error, got %v", err)
	}
}

func TestJobRepository_CreateUnit_AuditInsertError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	spec := domainjob.CreateSpec{
		Name:                 "test-job",
		TenantID:             "tenant-a",
		Priority:             5,
		TriggerType:          domainjob.TriggerTypeManual,
		Timezone:             "UTC",
		HandlerType:          "http",
		HandlerPayload:       map[string]any{},
		TimeoutSec:           60,
		RetryLimit:           3,
		RetryBackoffSec:      10,
		RetryBackoffStrategy: domainjob.RetryBackoffFixed,
		ConcurrencyPolicy:    domainjob.ConcurrencyAllow,
		MisfirePolicy:        domainjob.MisfireSkip,
	}

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").WithArgs("tenant-a").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("INSERT INTO jobs").
		WithArgs(
			"test-job", "tenant-a", 5, nil, "manual", (*string)(nil), "UTC", "http",
			sqlmock.AnyArg(), 60, 3, 10, "fixed", "allow", "skip", (*time.Time)(nil),
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "tenant_id", "status", "version", "next_run_at", "created_at", "updated_at",
		}).AddRow(int64(1), "test-job", "tenant-a", "active", 1, nil, now, now))

	// Audit insert fails — transaction rolls back and error is returned
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeSystem, "system", tenant.EventTypeJobCreated, tenant.ResourceTypeJob, "1", sqlmock.AnyArg()).
		WillReturnError(errors.New("audit boom"))
	mock.ExpectRollback()

	_, err = repo.Create(context.Background(), spec)
	if err == nil {
		t.Fatal("expected error on audit failure, got nil")
	}
	if !strings.Contains(err.Error(), "insert audit event") {
		t.Fatalf("expected insert audit event error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_CountActiveByTenantUnit_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)

	mock.ExpectQuery("SELECT count").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	count, err := repo.CountActiveByTenant(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("CountActiveByTenant() error = %v", err)
	}
	if count != 5 {
		t.Fatalf("expected count=5, got %d", count)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_CountActiveByTenantUnit_Error(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)

	mock.ExpectQuery("SELECT count").
		WithArgs("tenant-a").
		WillReturnError(errors.New("count boom"))

	_, err = repo.CountActiveByTenant(context.Background(), "tenant-a")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "count active jobs") {
		t.Fatalf("expected count active jobs error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func strPtr(s string) *string { return &s }

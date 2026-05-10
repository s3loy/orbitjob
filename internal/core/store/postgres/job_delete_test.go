package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	tenant "orbitjob/internal/core/domain/tenant"
)

func TestJobRepository_DeleteUnit_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()

	// set_config for RLS
	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery("UPDATE jobs").
		WithArgs("tenant-a", int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "tenant_id", "status", "version", "created_at", "updated_at",
		}).AddRow(int64(42), "demo-job", "tenant-a", "active", 1, now, now))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeAPIKey, "api_key", "job.deleted", tenant.ResourceTypeJob, "42", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	out, err := repo.Delete(context.Background(), "tenant-a", 42)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if out.ID != 42 {
		t.Fatalf("expected id=42, got %d", out.ID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_DeleteUnit_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)

	mock.ExpectBegin()

	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// UPDATE returns no rows
	mock.ExpectQuery("UPDATE jobs").
		WithArgs("tenant-a", int64(99)).
		WillReturnError(sql.ErrNoRows)

	// classifyJobWriteFailure: check if job exists → no rows
	mock.ExpectQuery("SELECT id FROM jobs").
		WithArgs("tenant-a", int64(99)).
		WillReturnError(sql.ErrNoRows)

	_, err = repo.Delete(context.Background(), "tenant-a", 99)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_DeleteUnit_Conflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)

	mock.ExpectBegin()

	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// UPDATE returns no rows (deleted_at already set or version mismatch)
	mock.ExpectQuery("UPDATE jobs").
		WithArgs("tenant-a", int64(42)).
		WillReturnError(sql.ErrNoRows)

	// classifyJobWriteFailure: job still exists → conflict
	mock.ExpectQuery("SELECT id FROM jobs").
		WithArgs("tenant-a", int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))

	_, err = repo.Delete(context.Background(), "tenant-a", 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale version conflict error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_DeleteUnit_BeginError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)

	mock.ExpectBegin().WillReturnError(errors.New("begin boom"))

	_, err = repo.Delete(context.Background(), "tenant-a", 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "begin job delete tx") {
		t.Fatalf("expected begin job delete tx error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_DeleteUnit_CommitError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()

	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery("UPDATE jobs").
		WithArgs("tenant-a", int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "tenant_id", "status", "version", "created_at", "updated_at",
		}).AddRow(int64(42), "demo-job", "tenant-a", "active", 1, now, now))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeAPIKey, "api_key", "job.deleted", tenant.ResourceTypeJob, "42", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit().WillReturnError(errors.New("commit boom"))

	_, err = repo.Delete(context.Background(), "tenant-a", 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "commit job delete tx") {
		t.Fatalf("expected commit job delete tx error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_DeleteUnit_SetTenantContextError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnError(errors.New("set_config boom"))
	mock.ExpectRollback()

	_, err = repo.Delete(context.Background(), "tenant-a", 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "set tenant context") {
		t.Fatalf("expected set tenant context error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_DeleteUnit_AuditInsertError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()

	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery("UPDATE jobs").
		WithArgs("tenant-a", int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "tenant_id", "status", "version", "created_at", "updated_at",
		}).AddRow(int64(42), "demo-job", "tenant-a", "active", 1, now, now))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeAPIKey, "api_key", "job.deleted", tenant.ResourceTypeJob, "42", sqlmock.AnyArg()).
		WillReturnError(errors.New("audit boom"))

	_, err = repo.Delete(context.Background(), "tenant-a", 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "insert audit event") {
		t.Fatalf("expected insert audit event error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

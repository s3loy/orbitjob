package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	domaininstance "orbitjob/internal/core/domain/instance"
)

func TestInstanceRepository_GetByIdempotencyKey_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewInstanceRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").WithArgs("tenant-a").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT (.+) FROM job_instances").
		WithArgs("tenant-a", "job_instance_create", "idem-1").
		WillReturnRows(sqlmock.NewRows(instanceColumnsAll).AddRow(
			int64(7), "run-idem", "tenant-a", int64(42), domaininstance.TriggerSourceManual,
			"pending", 5, 5, nil, "idem-1",
			"job_instance_create", nil, nil,
			1, 3,
			now, nil, nil,
			nil, nil, nil,
			nil, nil, nil,
			now, now,
			2,
		))
	mock.ExpectCommit()

	out, err := repo.GetByIdempotencyKey(context.Background(), "tenant-a", "job_instance_create", "idem-1")
	if err != nil {
		t.Fatalf("GetByIdempotencyKey() error = %v", err)
	}
	if out.ID != 7 {
		t.Fatalf("expected id=7, got %d", out.ID)
	}
	if out.RunID != "run-idem" {
		t.Fatalf("expected run_id=run-idem, got %q", out.RunID)
	}
	if out.IdempotencyKey == nil || *out.IdempotencyKey != "idem-1" {
		t.Fatalf("expected idempotency_key=idem-1, got %v", out.IdempotencyKey)
	}
	if out.Version != 2 {
		t.Fatalf("expected version=2, got %d", out.Version)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestInstanceRepository_GetByIdempotencyKey_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewInstanceRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").WithArgs("tenant-a").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT (.+) FROM job_instances").
		WithArgs("tenant-a", "job_instance_create", "missing").
		WillReturnError(errors.New("no rows"))
	mock.ExpectRollback()

	_, err = repo.GetByIdempotencyKey(context.Background(), "tenant-a", "job_instance_create", "missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !containsStr(err.Error(), "get instance by idempotency key") {
		t.Fatalf("expected wrapped error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

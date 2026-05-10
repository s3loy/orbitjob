package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	domainjob "orbitjob/internal/core/domain/job"
	tenant "orbitjob/internal/core/domain/tenant"
)

func TestJobRepository_ChangeStatusUnit_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	spec := domainjob.ChangeStatusSpec{
		ID:            42,
		TenantID:      "tenant-a",
		Version:       1,
		CurrentStatus: domainjob.StatusActive,
		NextStatus:    domainjob.StatusPaused,
		Action:        domainjob.ActionPause,
	}

	mock.ExpectBegin()

	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery("UPDATE jobs").
		WithArgs("tenant-a", int64(42), 1, "paused", "active").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "tenant_id", "status", "version", "next_run_at", "created_at", "updated_at",
		}).AddRow(int64(42), "test-job", "tenant-a", "paused", 2, nil, now, now))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeAPIKey, "control-plane-user", tenant.EventTypeJobStatusChanged, tenant.ResourceTypeJob, "42", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	out, err := repo.ChangeStatus(context.Background(), spec, "control-plane-user")
	if err != nil {
		t.Fatalf("ChangeStatus() error = %v", err)
	}
	if out.Status != domainjob.StatusPaused {
		t.Fatalf("expected status=paused, got %q", out.Status)
	}
	if out.Version != 2 {
		t.Fatalf("expected version=2, got %d", out.Version)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_ChangeStatusUnit_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)

	spec := domainjob.ChangeStatusSpec{
		ID:            99,
		TenantID:      "tenant-a",
		Version:       1,
		CurrentStatus: domainjob.StatusActive,
		NextStatus:    domainjob.StatusPaused,
		Action:        domainjob.ActionPause,
	}

	mock.ExpectBegin()

	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// UPDATE returns no rows (job doesn't exist)
	mock.ExpectQuery("UPDATE jobs").
		WithArgs("tenant-a", int64(99), 1, "paused", "active").
		WillReturnError(sql.ErrNoRows)

	// classifyJobWriteFailure: job not found
	mock.ExpectQuery("SELECT id FROM jobs").
		WithArgs("tenant-a", int64(99)).
		WillReturnError(sql.ErrNoRows)

	_, err = repo.ChangeStatus(context.Background(), spec, "control-plane-user")
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

func TestJobRepository_ChangeStatusUnit_Conflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)

	spec := domainjob.ChangeStatusSpec{
		ID:            42,
		TenantID:      "tenant-a",
		Version:       2,
		CurrentStatus: domainjob.StatusActive,
		NextStatus:    domainjob.StatusPaused,
		Action:        domainjob.ActionPause,
	}

	mock.ExpectBegin()

	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// UPDATE returns no rows (stale version)
	mock.ExpectQuery("UPDATE jobs").
		WithArgs("tenant-a", int64(42), 2, "paused", "active").
		WillReturnError(sql.ErrNoRows)

	// classifyJobWriteFailure: job exists → version conflict
	mock.ExpectQuery("SELECT id FROM jobs").
		WithArgs("tenant-a", int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))

	_, err = repo.ChangeStatus(context.Background(), spec, "control-plane-user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale version error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_ChangeStatusUnit_BeginError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)

	spec := domainjob.ChangeStatusSpec{
		ID:            42,
		TenantID:      "tenant-a",
		Version:       1,
		CurrentStatus: domainjob.StatusActive,
		NextStatus:    domainjob.StatusPaused,
		Action:        domainjob.ActionPause,
	}

	mock.ExpectBegin().WillReturnError(errors.New("begin boom"))

	_, err = repo.ChangeStatus(context.Background(), spec, "control-plane-user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "begin job status tx") {
		t.Fatalf("expected begin job status tx error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_ChangeStatusUnit_CommitError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	spec := domainjob.ChangeStatusSpec{
		ID:            42,
		TenantID:      "tenant-a",
		Version:       1,
		CurrentStatus: domainjob.StatusActive,
		NextStatus:    domainjob.StatusPaused,
		Action:        domainjob.ActionPause,
	}

	mock.ExpectBegin()

	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery("UPDATE jobs").
		WithArgs("tenant-a", int64(42), 1, "paused", "active").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "tenant_id", "status", "version", "next_run_at", "created_at", "updated_at",
		}).AddRow(int64(42), "test-job", "tenant-a", "paused", 2, nil, now, now))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeAPIKey, "control-plane-user", tenant.EventTypeJobStatusChanged, tenant.ResourceTypeJob, "42", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit().WillReturnError(errors.New("commit boom"))

	_, err = repo.ChangeStatus(context.Background(), spec, "control-plane-user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "commit job status tx") {
		t.Fatalf("expected commit job status tx error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_ChangeStatusUnit_AuditInsertError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	spec := domainjob.ChangeStatusSpec{
		ID:            42,
		TenantID:      "tenant-a",
		Version:       1,
		CurrentStatus: domainjob.StatusActive,
		NextStatus:    domainjob.StatusPaused,
		Action:        domainjob.ActionPause,
	}

	mock.ExpectBegin()

	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery("UPDATE jobs").
		WithArgs("tenant-a", int64(42), 1, "paused", "active").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "tenant_id", "status", "version", "next_run_at", "created_at", "updated_at",
		}).AddRow(int64(42), "test-job", "tenant-a", "paused", 2, nil, now, now))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeAPIKey, "control-plane-user", tenant.EventTypeJobStatusChanged, tenant.ResourceTypeJob, "42", sqlmock.AnyArg()).
		WillReturnError(errors.New("audit boom"))

	_, err = repo.ChangeStatus(context.Background(), spec, "control-plane-user")
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

func TestBuildStatusDiffPayload(t *testing.T) {
	in := domainjob.ChangeStatusSpec{
		ID:            42,
		TenantID:      "tenant-a",
		Version:       1,
		CurrentStatus: domainjob.StatusActive,
		NextStatus:    domainjob.StatusPaused,
		Action:        domainjob.ActionPause,
	}

	payload := buildStatusDiffPayload(in)

	if payload["from_version"] != 1 {
		t.Fatalf("expected from_version=1, got %v", payload["from_version"])
	}
	if payload["to_version"] != 2 {
		t.Fatalf("expected to_version=2, got %v", payload["to_version"])
	}
	if payload["from_status"] != "active" {
		t.Fatalf("expected from_status=active, got %v", payload["from_status"])
	}
	if payload["to_status"] != "paused" {
		t.Fatalf("expected to_status=paused, got %v", payload["to_status"])
	}
}

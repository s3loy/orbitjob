package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	domaininstance "orbitjob/internal/core/domain/instance"
	tenant "orbitjob/internal/core/domain/tenant"
)

func expectTenantContext(mock sqlmock.Sqlmock, tenantID string) {
	mock.ExpectExec("SELECT set_config").
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestInstanceRepository_CancelUnit_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewInstanceRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectTenantContext(mock, "tenant-a")

	mock.ExpectQuery("SELECT status FROM job_instances").
		WithArgs("tenant-a", "run-cancel").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(domaininstance.StatusDispatched))

	mock.ExpectQuery("UPDATE job_instances").
		WithArgs("tenant-a", "run-cancel", 2).
		WillReturnRows(sqlmock.NewRows(instanceColumnsAll).AddRow(
			int64(1), "run-cancel", "tenant-a", int64(42), "manual",
			"canceled", 5, 5, nil, nil,
			"job_instance_create", nil, nil,
			1, 3,
			now, nil, now, // scheduled_at, started_at, finished_at
			nil, nil, nil,
			nil, nil, nil,
			now, now,
			3,
		))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeAPIKey, "api_key", tenant.EventTypeInstanceStatusChanged, tenant.ResourceTypeInstance, "run-cancel", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	out, err := repo.Cancel(context.Background(), "tenant-a", "run-cancel", 2)
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if out.Status != domaininstance.StatusCanceled {
		t.Fatalf("expected status=canceled, got %q", out.Status)
	}
	if out.RunID != "run-cancel" {
		t.Fatalf("expected run_id=run-cancel, got %q", out.RunID)
	}
	if out.Version != 3 {
		t.Fatalf("expected version=3, got %d", out.Version)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestInstanceRepository_CancelUnit_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewInstanceRepository(db)

	mock.ExpectBegin()
	expectTenantContext(mock, "tenant-a")
	mock.ExpectQuery("SELECT status FROM job_instances").
		WithArgs("tenant-a", "run-missing").
		WillReturnError(sql.ErrNoRows)

	_, err = repo.Cancel(context.Background(), "tenant-a", "run-missing", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "read instance for cancel") {
		t.Fatalf("expected read instance for cancel error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestInstanceRepository_CancelUnit_OptimisticLockFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewInstanceRepository(db)

	mock.ExpectBegin()
	expectTenantContext(mock, "tenant-a")
	mock.ExpectQuery("SELECT status FROM job_instances").
		WithArgs("tenant-a", "run-stale").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(domaininstance.StatusDispatched))

	mock.ExpectQuery("UPDATE job_instances").
		WithArgs("tenant-a", "run-stale", 5).
		WillReturnError(sql.ErrNoRows)

	_, err = repo.Cancel(context.Background(), "tenant-a", "run-stale", 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cancel instance") {
		t.Fatalf("expected cancel instance error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestInstanceRepository_CancelUnit_BySourceStatus(t *testing.T) {
	tests := []struct {
		name        string
		fromStatus  string
		expectFound bool
	}{
		{"pending", domaininstance.StatusPending, true},
		{"retry_wait", domaininstance.StatusRetryWait, true},
		{"dispatched", domaininstance.StatusDispatched, true},
		{"running", domaininstance.StatusRunning, true},
		{"success", domaininstance.StatusSuccess, false},
		{"failed", domaininstance.StatusFailed, false},
		{"canceled", domaininstance.StatusCanceled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New() error = %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })

			repo := NewInstanceRepository(db)
			now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

			mock.ExpectBegin()
			expectTenantContext(mock, "tenant-a")

			mock.ExpectQuery("SELECT status FROM job_instances").
				WithArgs("tenant-a", "run-"+tt.fromStatus).
				WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(tt.fromStatus))

			if tt.expectFound {
				mock.ExpectQuery("UPDATE job_instances").
					WithArgs("tenant-a", "run-"+tt.fromStatus, 1).
					WillReturnRows(sqlmock.NewRows(instanceColumnsAll).AddRow(
						int64(1), "run-"+tt.fromStatus, "tenant-a", int64(42), "manual",
						"canceled", 5, 5, nil, nil,
						"job_instance_create", nil, nil,
						1, 3,
						now, nil, now,
						nil, nil, nil,
						nil, nil, nil,
						now, now,
						2,
					))

				mock.ExpectExec("INSERT INTO audit_events").
					WithArgs("tenant-a", tenant.ActorTypeAPIKey, "api_key", tenant.EventTypeInstanceStatusChanged, tenant.ResourceTypeInstance, "run-"+tt.fromStatus, sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectCommit()

				out, err := repo.Cancel(context.Background(), "tenant-a", "run-"+tt.fromStatus, 1)
				if err != nil {
					t.Fatalf("Cancel() error = %v", err)
				}
				if out.Status != domaininstance.StatusCanceled {
					t.Fatalf("expected status=canceled, got %q", out.Status)
				}
			} else {
				mock.ExpectQuery("UPDATE job_instances").
					WithArgs("tenant-a", "run-"+tt.fromStatus, 1).
					WillReturnError(sql.ErrNoRows)

				_, err := repo.Cancel(context.Background(), "tenant-a", "run-"+tt.fromStatus, 1)
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "cancel instance") {
					t.Fatalf("expected cancel instance error, got %v", err)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("sqlmock expectations: %v", err)
			}
		})
	}
}

func TestInstanceRepository_CancelUnit_BeginError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewInstanceRepository(db)

	mock.ExpectBegin().WillReturnError(errors.New("begin boom"))

	_, err = repo.Cancel(context.Background(), "tenant-a", "run-1", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "begin instance cancel tx") {
		t.Fatalf("expected begin instance cancel tx error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestInstanceRepository_CancelUnit_CommitError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewInstanceRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectTenantContext(mock, "tenant-a")

	mock.ExpectQuery("SELECT status FROM job_instances").
		WithArgs("tenant-a", "run-1").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(domaininstance.StatusDispatched))

	mock.ExpectQuery("UPDATE job_instances").
		WithArgs("tenant-a", "run-1", 1).
		WillReturnRows(sqlmock.NewRows(instanceColumnsAll).AddRow(
			int64(1), "run-1", "tenant-a", int64(42), "manual",
			"canceled", 5, 5, nil, nil,
			"job_instance_create", nil, nil,
			1, 3,
			now, nil, now,
			nil, nil, nil,
			nil, nil, nil,
			now, now,
			2,
		))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeAPIKey, "api_key", tenant.EventTypeInstanceStatusChanged, tenant.ResourceTypeInstance, "run-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit().WillReturnError(errors.New("commit boom"))

	_, err = repo.Cancel(context.Background(), "tenant-a", "run-1", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "commit instance cancel tx") {
		t.Fatalf("expected commit instance cancel tx error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestInstanceRepository_CancelUnit_AuditInsertError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewInstanceRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectTenantContext(mock, "tenant-a")

	mock.ExpectQuery("SELECT status FROM job_instances").
		WithArgs("tenant-a", "run-1").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(domaininstance.StatusDispatched))

	mock.ExpectQuery("UPDATE job_instances").
		WithArgs("tenant-a", "run-1", 1).
		WillReturnRows(sqlmock.NewRows(instanceColumnsAll).AddRow(
			int64(1), "run-1", "tenant-a", int64(42), "manual",
			"canceled", 5, 5, nil, nil,
			"job_instance_create", nil, nil,
			1, 3,
			now, nil, now,
			nil, nil, nil,
			nil, nil, nil,
			now, now,
			2,
		))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeAPIKey, "api_key", tenant.EventTypeInstanceStatusChanged, tenant.ResourceTypeInstance, "run-1", sqlmock.AnyArg()).
		WillReturnError(errors.New("audit boom"))

	_, err = repo.Cancel(context.Background(), "tenant-a", "run-1", 1)
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

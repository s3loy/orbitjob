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
)

func newExecutorRepoMock(t *testing.T) (*ExecutorRepository, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return NewExecutorRepository(db), mock
}

var assignedTaskColumns = []string{
	"id", "run_id", "tenant_id", "job_id",
	"handler_type", "handler_payload", "timeout_sec",
	"retry_backoff_sec", "retry_backoff_strategy",
	"priority", "attempt", "max_attempt",
	"trace_id", "scheduled_at", "lease_expires_at",
}

// ---------------------------------------------------------------------------
// FetchAssigned
// ---------------------------------------------------------------------------

func TestFetchAssigned_ReturnsTask(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	lease := now.Add(60 * time.Second)

	rows := sqlmock.NewRows(assignedTaskColumns).AddRow(
		int64(1), "run-abc", "tenant-a", int64(42),
		"exec", []byte(`{"command":"echo","args":["hello"]}`), 30,
		10, "fixed",
		5, 1, 3,
		nil, now, lease,
	)
	mock.ExpectQuery("SELECT ji.id").
		WithArgs("tenant-a", "worker-1", 1).
		WillReturnRows(rows)

	tasks, err := repo.FetchAssigned(context.Background(), "tenant-a", "worker-1", 1)
	if err != nil {
		t.Fatalf("FetchAssigned() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.InstanceID != 1 {
		t.Fatalf("expected instance_id=1, got %d", task.InstanceID)
	}
	if task.HandlerType != "exec" {
		t.Fatalf("expected handler_type=exec, got %q", task.HandlerType)
	}
	if task.HandlerPayload["command"] != "echo" {
		t.Fatalf("expected command=echo, got %v", task.HandlerPayload["command"])
	}
	if task.TimeoutSec != 30 {
		t.Fatalf("expected timeout_sec=30, got %d", task.TimeoutSec)
	}
	if task.RetryBackoffSec != 10 {
		t.Fatalf("expected retry_backoff_sec=10, got %d", task.RetryBackoffSec)
	}
	assertMock(t, mock)
}

func TestFetchAssigned_Empty(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)

	mock.ExpectQuery("SELECT ji.id").
		WithArgs("tenant-a", "worker-1", 1).
		WillReturnRows(sqlmock.NewRows(assignedTaskColumns))

	tasks, err := repo.FetchAssigned(context.Background(), "tenant-a", "worker-1", 1)
	if err != nil {
		t.Fatalf("FetchAssigned() error = %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
	assertMock(t, mock)
}

func TestFetchAssigned_QueryError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)

	mock.ExpectQuery("SELECT ji.id").
		WithArgs("tenant-a", "worker-1", 1).
		WillReturnError(errors.New("db boom"))

	_, err := repo.FetchAssigned(context.Background(), "tenant-a", "worker-1", 1)
	if err == nil || !strings.Contains(err.Error(), "fetch assigned") {
		t.Fatalf("expected fetch assigned error, got %v", err)
	}
	assertMock(t, mock)
}

// ---------------------------------------------------------------------------
// StartInstance
// ---------------------------------------------------------------------------

func TestStartInstance_Success(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE job_instances").
		WithArgs(now, "tenant-a", int64(1), "worker-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.StartInstance(context.Background(), domaininstance.StartSpec{
		TenantID:   "tenant-a",
		InstanceID: 1,
		WorkerID:   "worker-1",
		StartedAt:  now,
	})
	if err != nil {
		t.Fatalf("StartInstance() error = %v", err)
	}
	assertMock(t, mock)
}

func TestStartInstance_NotClaimed(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE job_instances").
		WithArgs(now, "tenant-a", int64(1), "worker-1").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.StartInstance(context.Background(), domaininstance.StartSpec{
		TenantID:   "tenant-a",
		InstanceID: 1,
		WorkerID:   "worker-1",
		StartedAt:  now,
	})
	if !errors.Is(err, ErrInstanceNotClaimed) {
		t.Fatalf("expected ErrInstanceNotClaimed, got %v", err)
	}
	assertMock(t, mock)
}

func TestStartInstance_DBError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE job_instances").
		WithArgs(now, "tenant-a", int64(1), "worker-1").
		WillReturnError(errors.New("start boom"))

	err := repo.StartInstance(context.Background(), domaininstance.StartSpec{
		TenantID:   "tenant-a",
		InstanceID: 1,
		WorkerID:   "worker-1",
		StartedAt:  now,
	})
	if err == nil || !strings.Contains(err.Error(), "start instance") {
		t.Fatalf("expected start instance error, got %v", err)
	}
	assertMock(t, mock)
}

// ---------------------------------------------------------------------------
// CompleteInstance
// ---------------------------------------------------------------------------

func TestCompleteInstance_Success(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	resultCode := "0"

	mock.ExpectExec("UPDATE job_instances").
		WithArgs("success", now, &resultCode, nil, nil, "tenant-a", int64(1), "worker-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.CompleteInstance(context.Background(), domaininstance.CompleteSpec{
		TenantID:   "tenant-a",
		InstanceID: 1,
		WorkerID:   "worker-1",
		Status:     "success",
		ResultCode: &resultCode,
		FinishedAt: now,
	})
	if err != nil {
		t.Fatalf("CompleteInstance() error = %v", err)
	}
	assertMock(t, mock)
}

func TestCompleteInstance_RetryWait(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	retryAt := now.Add(10 * time.Second)
	resultCode := "1"
	errorMsg := "some error"

	mock.ExpectExec("UPDATE job_instances").
		WithArgs("retry_wait", now, &resultCode, &errorMsg, &retryAt, "tenant-a", int64(1), "worker-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.CompleteInstance(context.Background(), domaininstance.CompleteSpec{
		TenantID:   "tenant-a",
		InstanceID: 1,
		WorkerID:   "worker-1",
		Status:     "retry_wait",
		ResultCode: &resultCode,
		ErrorMsg:   &errorMsg,
		FinishedAt: now,
		RetryAt:    &retryAt,
	})
	if err != nil {
		t.Fatalf("CompleteInstance() error = %v", err)
	}
	assertMock(t, mock)
}

func TestCompleteInstance_NotClaimed(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE job_instances").
		WithArgs("success", now, nil, nil, nil, "tenant-a", int64(1), "worker-1").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.CompleteInstance(context.Background(), domaininstance.CompleteSpec{
		TenantID:   "tenant-a",
		InstanceID: 1,
		WorkerID:   "worker-1",
		Status:     "success",
		FinishedAt: now,
	})
	if !errors.Is(err, ErrInstanceNotClaimed) {
		t.Fatalf("expected ErrInstanceNotClaimed, got %v", err)
	}
	assertMock(t, mock)
}

// ---------------------------------------------------------------------------
// ExtendLease
// ---------------------------------------------------------------------------

func TestExtendLease_Success(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	newExpiry := time.Date(2026, 4, 20, 12, 1, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE job_instances").
		WithArgs(newExpiry, "tenant-a", int64(1), "worker-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.ExtendLease(context.Background(), "tenant-a", 1, "worker-1", newExpiry)
	if err != nil {
		t.Fatalf("ExtendLease() error = %v", err)
	}
	assertMock(t, mock)
}

func TestExtendLease_NotClaimed(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	newExpiry := time.Date(2026, 4, 20, 12, 1, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE job_instances").
		WithArgs(newExpiry, "tenant-a", int64(1), "worker-1").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.ExtendLease(context.Background(), "tenant-a", 1, "worker-1", newExpiry)
	if !errors.Is(err, ErrInstanceNotClaimed) {
		t.Fatalf("expected ErrInstanceNotClaimed, got %v", err)
	}
	assertMock(t, mock)
}

func TestNewExecutorRepository(t *testing.T) {
	db := &sql.DB{}
	repo := NewExecutorRepository(db)
	if repo == nil {
		t.Fatal("expected repo != nil")
	}
	if repo.db != db {
		t.Fatal("expected repository to keep db reference")
	}
}

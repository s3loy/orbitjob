package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

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

var claimTaskColumns = []string{
	"id", "run_id", "tenant_id", "job_id",
	"handler_type", "handler_payload", "timeout_sec",
	"retry_backoff_sec", "retry_backoff_strategy",
	"priority", "effective_priority",
	"attempt", "max_attempt",
	"trace_id", "scheduled_at", "dispatched_at", "lease_expires_at", "started_at",
}

func expectSetTenantContext(mock sqlmock.Sqlmock, tenantID string) {
	mock.ExpectExec("SELECT set_config").
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectAuditInsertExecutor(mock sqlmock.Sqlmock, tenantID, resourceID, eventType string) {
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(tenantID, "system", "worker", eventType, "instance", resourceID, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectAttemptInsert(mock sqlmock.Sqlmock, tenantID string, instanceID int64, attempt int, workerID, status string, startedAt time.Time) {
	mock.ExpectExec("INSERT INTO job_instance_attempts").
		WithArgs(tenantID, instanceID, attempt, workerID, status, startedAt, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

// ---------------------------------------------------------------------------
// ClaimNextDispatched
// ---------------------------------------------------------------------------

func TestClaimNextDispatched_ClaimsTask(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	lease := now.Add(60 * time.Second)
	dispatchedAt := now.Add(-5 * time.Second)

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	rows := sqlmock.NewRows(claimTaskColumns).AddRow(
		int64(1), "run-abc", "tenant-a", int64(42),
		"exec", []byte(`{"command":"echo","args":["hello"]}`), 30,
		10, "fixed",
		5, 5, 1, 3,
		nil, now, dispatchedAt, lease, now,
	)
	mock.ExpectQuery("WITH claimed").
		WithArgs("tenant-a", 1, "worker-1", now, lease, sqlmock.AnyArg()).
		WillReturnRows(rows)
	expectAuditInsertExecutor(mock, "tenant-a", "1", "instance.status_changed")
	mock.ExpectCommit()

	tasks, err := repo.ClaimNextDispatched(context.Background(), "tenant-a", "worker-1", 1, lease, now, nil)
	if err != nil {
		t.Fatalf("ClaimNextDispatched() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.InstanceID != 1 {
		t.Fatalf("expected instance_id=1, got %d", task.InstanceID)
	}
	if task.EffectivePriority != 5 {
		t.Fatalf("expected effective_priority=5, got %d", task.EffectivePriority)
	}
	if !task.DispatchedAt.Equal(dispatchedAt) {
		t.Fatalf("expected dispatched_at=%v, got %v", dispatchedAt, task.DispatchedAt)
	}
	if !task.StartedAt.Equal(now) {
		t.Fatalf("expected started_at=%v, got %v", now, task.StartedAt)
	}
	assertMock(t, mock)
}

func TestClaimNextDispatched_Empty(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Now().UTC()
	lease := now.Add(60 * time.Second)

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	mock.ExpectQuery("WITH claimed").
		WithArgs("tenant-a", 1, "worker-1", now, lease, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(claimTaskColumns))
	mock.ExpectCommit()

	tasks, err := repo.ClaimNextDispatched(context.Background(), "tenant-a", "worker-1", 1, lease, now, nil)
	if err != nil {
		t.Fatalf("ClaimNextDispatched() error = %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
	assertMock(t, mock)
}

func TestClaimNextDispatched_QueryError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Now().UTC()
	lease := now.Add(60 * time.Second)

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	mock.ExpectQuery("WITH claimed").
		WithArgs("tenant-a", 1, "worker-1", now, lease, sqlmock.AnyArg()).
		WillReturnError(errors.New("db boom"))
	mock.ExpectRollback()

	_, err := repo.ClaimNextDispatched(context.Background(), "tenant-a", "worker-1", 1, lease, now, nil)
	if err == nil || !strings.Contains(err.Error(), "claim dispatched") {
		t.Fatalf("expected claim dispatched error, got %v", err)
	}
	assertMock(t, mock)
}

func TestClaimNextDispatched_WithRoutingKeyLabels(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	lease := now.Add(60 * time.Second)
	dispatchedAt := now.Add(-5 * time.Second)
	labels := map[string]any{"queue": "video", "zone": 5}

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	rows := sqlmock.NewRows(claimTaskColumns).AddRow(
		int64(1), "run-abc", "tenant-a", int64(42),
		"exec", []byte(`{"command":"echo","args":["hello"]}`), 30,
		10, "fixed",
		5, 5, 1, 3,
		nil, now, dispatchedAt, lease, now,
	)
	mock.ExpectQuery("WITH claimed").
		WithArgs("tenant-a", 1, "worker-1", now, lease, pq.Array([]string{"video"})).
		WillReturnRows(rows)
	expectAuditInsertExecutor(mock, "tenant-a", "1", "instance.status_changed")
	mock.ExpectCommit()

	tasks, err := repo.ClaimNextDispatched(context.Background(), "tenant-a", "worker-1", 1, lease, now, labels)
	if err != nil {
		t.Fatalf("ClaimNextDispatched() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	assertMock(t, mock)
}


func TestCompleteInstance_Success(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	startedAt := now.Add(-30 * time.Second)
	resultCode := "0"

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	mock.ExpectQuery("UPDATE job_instances").
		WithArgs("success", now, &resultCode, nil, nil, "tenant-a", int64(1), "worker-1").
		WillReturnRows(sqlmock.NewRows([]string{"started_at"}).AddRow(startedAt))
	expectAttemptInsert(mock, "tenant-a", 1, 1, "worker-1", "success", startedAt)
	expectAuditInsertExecutor(mock, "tenant-a", "1", "instance.completed")
	mock.ExpectCommit()

	err := repo.CompleteInstance(context.Background(), domaininstance.CompleteSpec{
		TenantID:   "tenant-a",
		InstanceID: 1,
		WorkerID:   "worker-1",
		Status:     "success",
		Attempt:    1,
		ResultCode: &resultCode,
		StartedAt:  startedAt,
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
	startedAt := now.Add(-30 * time.Second)
	retryAt := now.Add(10 * time.Second)
	resultCode := "1"
	errorMsg := "some error"

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	mock.ExpectQuery("UPDATE job_instances").
		WithArgs("retry_wait", now, &resultCode, &errorMsg, &retryAt, "tenant-a", int64(1), "worker-1").
		WillReturnRows(sqlmock.NewRows([]string{"started_at"}).AddRow(startedAt))
	expectAttemptInsert(mock, "tenant-a", 1, 1, "worker-1", "failed", startedAt)
	expectAuditInsertExecutor(mock, "tenant-a", "1", "instance.status_changed")
	mock.ExpectCommit()

	err := repo.CompleteInstance(context.Background(), domaininstance.CompleteSpec{
		TenantID:   "tenant-a",
		InstanceID: 1,
		WorkerID:   "worker-1",
		Status:     "retry_wait",
		Attempt:    1,
		ResultCode: &resultCode,
		ErrorMsg:   &errorMsg,
		StartedAt:  startedAt,
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

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	mock.ExpectQuery("UPDATE job_instances").
		WithArgs("success", now, nil, nil, nil, "tenant-a", int64(1), "worker-1").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

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

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	mock.ExpectExec("UPDATE job_instances").
		WithArgs(newExpiry, "tenant-a", int64(1), "worker-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectAuditInsertExecutor(mock, "tenant-a", "1", "instance.status_changed")
	mock.ExpectCommit()

	err := repo.ExtendLease(context.Background(), "tenant-a", 1, "worker-1", newExpiry)
	if err != nil {
		t.Fatalf("ExtendLease() error = %v", err)
	}
	assertMock(t, mock)
}

func TestExtendLease_NotClaimed(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	newExpiry := time.Date(2026, 4, 20, 12, 1, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	mock.ExpectExec("UPDATE job_instances").
		WithArgs(newExpiry, "tenant-a", int64(1), "worker-1").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

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

// ---------------------------------------------------------------------------
// ClaimNextDispatched additional error paths
// ---------------------------------------------------------------------------

func TestClaimNextDispatched_BeginError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Now().UTC()
	lease := now.Add(60 * time.Second)

	mock.ExpectBegin().WillReturnError(errors.New("begin boom"))

	_, err := repo.ClaimNextDispatched(context.Background(), "tenant-a", "worker-1", 1, lease, now, nil)
	if err == nil || !strings.Contains(err.Error(), "begin claim tx") {
		t.Fatalf("expected begin claim tx error, got %v", err)
	}
	assertMock(t, mock)
}

func TestClaimNextDispatched_SetTenantContextError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Now().UTC()
	lease := now.Add(60 * time.Second)

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnError(errors.New("set_config boom"))
	mock.ExpectRollback()

	_, err := repo.ClaimNextDispatched(context.Background(), "tenant-a", "worker-1", 1, lease, now, nil)
	if err == nil || !strings.Contains(err.Error(), "set tenant context") {
		t.Fatalf("expected set tenant context error, got %v", err)
	}
	assertMock(t, mock)
}

func TestClaimNextDispatched_RowsErr(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Now().UTC()
	lease := now.Add(60 * time.Second)
	dispatchedAt := now.Add(-5 * time.Second)

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	// Return a row and a RowError to trigger rows.Err()
	rows := sqlmock.NewRows(claimTaskColumns).AddRow(
		int64(1), "run-abc", "tenant-a", int64(42),
		"exec", []byte(`{"command":"echo","args":["hello"]}`), 30,
		10, "fixed",
		5, 5, 1, 3,
		nil, now, dispatchedAt, lease, now,
	).RowError(0, errors.New("rows err boom"))
	mock.ExpectQuery("WITH claimed").
		WithArgs("tenant-a", 1, "worker-1", now, lease, sqlmock.AnyArg()).
		WillReturnRows(rows)
	mock.ExpectRollback()

	_, err := repo.ClaimNextDispatched(context.Background(), "tenant-a", "worker-1", 1, lease, now, nil)
	if err == nil || !strings.Contains(err.Error(), "iterate claimed instances") {
		t.Fatalf("expected iterate claimed instances error, got %v", err)
	}
}

func TestClaimNextDispatched_AuditInsertError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Now().UTC()
	lease := now.Add(60 * time.Second)
	dispatchedAt := now.Add(-5 * time.Second)

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	rows := sqlmock.NewRows(claimTaskColumns).AddRow(
		int64(1), "run-abc", "tenant-a", int64(42),
		"exec", []byte(`{"command":"echo","args":["hello"]}`), 30,
		10, "fixed",
		5, 5, 1, 3,
		nil, now, dispatchedAt, lease, now,
	)
	mock.ExpectQuery("WITH claimed").
		WithArgs("tenant-a", 1, "worker-1", now, lease, sqlmock.AnyArg()).
		WillReturnRows(rows)

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", "system", "worker", "instance.status_changed", "instance", "1", sqlmock.AnyArg()).
		WillReturnError(errors.New("audit boom"))
	mock.ExpectRollback()

	_, err := repo.ClaimNextDispatched(context.Background(), "tenant-a", "worker-1", 1, lease, now, nil)
	if err == nil || !strings.Contains(err.Error(), "insert claim audit event") {
		t.Fatalf("expected insert claim audit event error, got %v", err)
	}
}

func TestClaimNextDispatched_CommitError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Now().UTC()
	lease := now.Add(60 * time.Second)
	dispatchedAt := now.Add(-5 * time.Second)

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	rows := sqlmock.NewRows(claimTaskColumns).AddRow(
		int64(1), "run-abc", "tenant-a", int64(42),
		"exec", []byte(`{"command":"echo","args":["hello"]}`), 30,
		10, "fixed",
		5, 5, 1, 3,
		nil, now, dispatchedAt, lease, now,
	)
	mock.ExpectQuery("WITH claimed").
		WithArgs("tenant-a", 1, "worker-1", now, lease, sqlmock.AnyArg()).
		WillReturnRows(rows)
	expectAuditInsertExecutor(mock, "tenant-a", "1", "instance.status_changed")
	mock.ExpectCommit().WillReturnError(errors.New("commit boom"))

	_, err := repo.ClaimNextDispatched(context.Background(), "tenant-a", "worker-1", 1, lease, now, nil)
	if err == nil || !strings.Contains(err.Error(), "commit claim tx") {
		t.Fatalf("expected commit claim tx error, got %v", err)
	}
	assertMock(t, mock)
}

// ---------------------------------------------------------------------------
// CompleteInstance additional error paths
// ---------------------------------------------------------------------------

func TestCompleteInstance_BeginError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Now().UTC()

	mock.ExpectBegin().WillReturnError(errors.New("begin boom"))

	err := repo.CompleteInstance(context.Background(), domaininstance.CompleteSpec{
		TenantID:   "tenant-a",
		InstanceID: 1,
		WorkerID:   "worker-1",
		Status:     "success",
		FinishedAt: now,
	})
	if err == nil || !strings.Contains(err.Error(), "begin complete tx") {
		t.Fatalf("expected begin complete tx error, got %v", err)
	}
	assertMock(t, mock)
}

func TestCompleteInstance_SetTenantContextError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnError(errors.New("set_config boom"))
	mock.ExpectRollback()

	err := repo.CompleteInstance(context.Background(), domaininstance.CompleteSpec{
		TenantID:   "tenant-a",
		InstanceID: 1,
		WorkerID:   "worker-1",
		Status:     "success",
		FinishedAt: now,
	})
	if err == nil || !strings.Contains(err.Error(), "set tenant context") {
		t.Fatalf("expected set tenant context error, got %v", err)
	}
	assertMock(t, mock)
}

func TestCompleteInstance_QueryError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Now().UTC()

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	mock.ExpectQuery("UPDATE job_instances").
		WithArgs("success", now, nil, nil, nil, "tenant-a", int64(1), "worker-1").
		WillReturnError(errors.New("exec boom"))
	mock.ExpectRollback()

	err := repo.CompleteInstance(context.Background(), domaininstance.CompleteSpec{
		TenantID:   "tenant-a",
		InstanceID: 1,
		WorkerID:   "worker-1",
		Status:     "success",
		FinishedAt: now,
	})
	if err == nil || !strings.Contains(err.Error(), "complete instance") {
		t.Fatalf("expected complete instance error, got %v", err)
	}
	assertMock(t, mock)
}

func TestCompleteInstance_AttemptInsertError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Now().UTC()
	startedAt := now.Add(-30 * time.Second)

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	mock.ExpectQuery("UPDATE job_instances").
		WithArgs("success", now, nil, nil, nil, "tenant-a", int64(1), "worker-1").
		WillReturnRows(sqlmock.NewRows([]string{"started_at"}).AddRow(startedAt))
	mock.ExpectExec("INSERT INTO job_instance_attempts").
		WithArgs("tenant-a", int64(1), 1, "worker-1", "success", startedAt, now, nil, nil).
		WillReturnError(errors.New("attempt insert boom"))
	mock.ExpectRollback()

	err := repo.CompleteInstance(context.Background(), domaininstance.CompleteSpec{
		TenantID:   "tenant-a",
		InstanceID: 1,
		WorkerID:   "worker-1",
		Status:     "success",
		Attempt:    1,
		StartedAt:  startedAt,
		FinishedAt: now,
	})
	if err == nil || !strings.Contains(err.Error(), "insert instance attempt") {
		t.Fatalf("expected insert instance attempt error, got %v", err)
	}
}

func TestCompleteInstance_AuditInsertError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Now().UTC()
	startedAt := now.Add(-30 * time.Second)

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	mock.ExpectQuery("UPDATE job_instances").
		WithArgs("success", now, nil, nil, nil, "tenant-a", int64(1), "worker-1").
		WillReturnRows(sqlmock.NewRows([]string{"started_at"}).AddRow(startedAt))
	expectAttemptInsert(mock, "tenant-a", 1, 1, "worker-1", "success", startedAt)
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", "system", "worker", "instance.completed", "instance", "1", sqlmock.AnyArg()).
		WillReturnError(errors.New("audit boom"))
	mock.ExpectRollback()

	err := repo.CompleteInstance(context.Background(), domaininstance.CompleteSpec{
		TenantID:   "tenant-a",
		InstanceID: 1,
		WorkerID:   "worker-1",
		Status:     "success",
		Attempt:    1,
		StartedAt:  startedAt,
		FinishedAt: now,
	})
	if err == nil || !strings.Contains(err.Error(), "insert audit event") {
		t.Fatalf("expected insert audit event error, got %v", err)
	}
	assertMock(t, mock)
}

func TestCompleteInstance_CommitError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	now := time.Now().UTC()
	startedAt := now.Add(-30 * time.Second)

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	mock.ExpectQuery("UPDATE job_instances").
		WithArgs("success", now, nil, nil, nil, "tenant-a", int64(1), "worker-1").
		WillReturnRows(sqlmock.NewRows([]string{"started_at"}).AddRow(startedAt))
	expectAttemptInsert(mock, "tenant-a", 1, 1, "worker-1", "success", startedAt)
	expectAuditInsertExecutor(mock, "tenant-a", "1", "instance.completed")
	mock.ExpectCommit().WillReturnError(errors.New("commit boom"))

	err := repo.CompleteInstance(context.Background(), domaininstance.CompleteSpec{
		TenantID:   "tenant-a",
		InstanceID: 1,
		WorkerID:   "worker-1",
		Status:     "success",
		Attempt:    1,
		StartedAt:  startedAt,
		FinishedAt: now,
	})
	if err == nil || !strings.Contains(err.Error(), "commit complete tx") {
		t.Fatalf("expected commit complete tx error, got %v", err)
	}
	assertMock(t, mock)
}

// ---------------------------------------------------------------------------
// ExtendLease additional error paths
// ---------------------------------------------------------------------------

func TestExtendLease_SetTenantContextError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	newExpiry := time.Date(2026, 4, 20, 12, 1, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnError(errors.New("set_config boom"))
	mock.ExpectRollback()

	err := repo.ExtendLease(context.Background(), "tenant-a", 1, "worker-1", newExpiry)
	if err == nil || !strings.Contains(err.Error(), "set tenant context") {
		t.Fatalf("expected set tenant context error, got %v", err)
	}
	assertMock(t, mock)
}

func TestExtendLease_ExecError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	newExpiry := time.Date(2026, 4, 20, 12, 1, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	mock.ExpectExec("UPDATE job_instances").
		WithArgs(newExpiry, "tenant-a", int64(1), "worker-1").
		WillReturnError(errors.New("exec boom"))
	mock.ExpectRollback()

	err := repo.ExtendLease(context.Background(), "tenant-a", 1, "worker-1", newExpiry)
	if err == nil || !strings.Contains(err.Error(), "extend lease") {
		t.Fatalf("expected extend lease error, got %v", err)
	}
	assertMock(t, mock)
}

func TestExtendLease_RowsAffectedError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	newExpiry := time.Date(2026, 4, 20, 12, 1, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	mock.ExpectExec("UPDATE job_instances").
		WithArgs(newExpiry, "tenant-a", int64(1), "worker-1").
		WillReturnResult(sqlmock.NewErrorResult(errors.New("rows affected boom")))
	mock.ExpectRollback()

	err := repo.ExtendLease(context.Background(), "tenant-a", 1, "worker-1", newExpiry)
	if err == nil || !strings.Contains(err.Error(), "extend lease rows affected") {
		t.Fatalf("expected extend lease rows affected error, got %v", err)
	}
	assertMock(t, mock)
}

func TestExtendLease_AuditInsertError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)
	newExpiry := time.Date(2026, 4, 20, 12, 1, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectSetTenantContext(mock, "tenant-a")
	mock.ExpectExec("UPDATE job_instances").
		WithArgs(newExpiry, "tenant-a", int64(1), "worker-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", "system", "worker", "instance.status_changed", "instance", "1", sqlmock.AnyArg()).
		WillReturnError(errors.New("audit boom"))
	mock.ExpectRollback()

	err := repo.ExtendLease(context.Background(), "tenant-a", 1, "worker-1", newExpiry)
	if err == nil || !strings.Contains(err.Error(), "insert lease audit event") {
		t.Fatalf("expected insert lease audit event error, got %v", err)
	}
	assertMock(t, mock)
}

// ---------------------------------------------------------------------------
// scanAssignedTask
// ---------------------------------------------------------------------------

func TestScanAssignedTask_UnmarshalError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC()
	// Malformed JSON in handler_payload
	rows := sqlmock.NewRows(claimTaskColumns).AddRow(
		int64(1), "run-abc", "tenant-a", int64(42),
		"exec", []byte("{bad json"), 30,
		10, "fixed",
		5, 5, 1, 3,
		nil, now, nil, nil, now,
	)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	r, err := db.Query("SELECT")
	if err != nil {
		t.Fatalf("Query error = %v", err)
	}
	defer func() { _ = r.Close() }()

	if !r.Next() {
		t.Fatal("expected a row")
	}
	_, err = scanAssignedTask(r)
	if err == nil || !strings.Contains(err.Error(), "unmarshal handler_payload") {
		t.Fatalf("expected unmarshal handler_payload error, got %v", err)
	}
}

func TestScanAssignedTask_NullPayloadJSON(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC()
	// JSON null as payload → unmarshals to nil, then gets replaced with empty map
	rows := sqlmock.NewRows(claimTaskColumns).AddRow(
		int64(1), "run-abc", "tenant-a", int64(42),
		"exec", []byte("null"), 30,
		10, "fixed",
		5, 5, 1, 3,
		nil, now, nil, nil, now,
	)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	r, err := db.Query("SELECT")
	if err != nil {
		t.Fatalf("Query error = %v", err)
	}
	defer func() { _ = r.Close() }()

	if !r.Next() {
		t.Fatal("expected a row")
	}
	task, err := scanAssignedTask(r)
	if err != nil {
		t.Fatalf("scanAssignedTask() error = %v", err)
	}
	if task.HandlerPayload == nil {
		t.Fatal("expected non-nil handler_payload (null should be replaced with empty map)")
	}
	if len(task.HandlerPayload) != 0 {
		t.Fatalf("expected empty handler_payload, got %v", task.HandlerPayload)
	}
}

func TestScanAssignedTask_NullFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC()
	// Null trace_id, null dispatched_at, null lease_expires_at, empty payload bytes
	rows := sqlmock.NewRows(claimTaskColumns).AddRow(
		int64(1), "run-abc", "tenant-a", int64(42),
		"exec", []byte{}, 30,
		10, "fixed",
		5, 5, 1, 3,
		nil, now, nil, nil, now,
	)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	r, err := db.Query("SELECT")
	if err != nil {
		t.Fatalf("Query error = %v", err)
	}
	defer func() { _ = r.Close() }()

	if !r.Next() {
		t.Fatal("expected a row")
	}
	task, err := scanAssignedTask(r)
	if err != nil {
		t.Fatalf("scanAssignedTask() error = %v", err)
	}
	if task.TraceID != nil {
		t.Fatalf("expected nil trace_id, got %v", *task.TraceID)
	}
	if !task.LeaseExpiresAt.IsZero() {
		t.Fatalf("expected zero lease_expires_at, got %v", task.LeaseExpiresAt)
	}
	if !task.DispatchedAt.IsZero() {
		t.Fatalf("expected zero dispatched_at, got %v", task.DispatchedAt)
	}
	if task.HandlerPayload == nil {
		t.Fatal("expected non-nil handler_payload (initialized to empty map)")
	}
	if len(task.HandlerPayload) != 0 {
		t.Fatalf("expected empty handler_payload, got %v", task.HandlerPayload)
	}
}

func TestExecutorRepository_ListActiveTenantIDs_Success(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)

	rows := sqlmock.NewRows([]string{"id"}).AddRow("tenant-a").AddRow("tenant-b")
	mock.ExpectQuery("SELECT id FROM tenants WHERE status = 'active' ORDER BY id").
		WillReturnRows(rows)

	ids, err := repo.ListActiveTenantIDs(context.Background())
	if err != nil {
		t.Fatalf("ListActiveTenantIDs() error = %v", err)
	}
	if len(ids) != 2 || ids[0] != "tenant-a" || ids[1] != "tenant-b" {
		t.Fatalf("expected [tenant-a tenant-b], got %v", ids)
	}
	assertMock(t, mock)
}

func TestExecutorRepository_ListActiveTenantIDs_Empty(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)

	rows := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery("SELECT id FROM tenants WHERE status = 'active' ORDER BY id").
		WillReturnRows(rows)

	ids, err := repo.ListActiveTenantIDs(context.Background())
	if err != nil {
		t.Fatalf("ListActiveTenantIDs() error = %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty slice, got %v", ids)
	}
	assertMock(t, mock)
}

func TestExecutorRepository_ListActiveTenantIDs_QueryError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)

	mock.ExpectQuery("SELECT id FROM tenants WHERE status = 'active' ORDER BY id").
		WillReturnError(errors.New("db down"))

	_, err := repo.ListActiveTenantIDs(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	assertMock(t, mock)
}

func TestExecutorRepository_ListActiveTenantIDs_ScanError(t *testing.T) {
	repo, mock := newExecutorRepoMock(t)

	rows := sqlmock.NewRows([]string{"id"}).AddRow(nil)
	mock.ExpectQuery("SELECT id FROM tenants WHERE status = 'active' ORDER BY id").
		WillReturnRows(rows)

	_, err := repo.ListActiveTenantIDs(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	assertMock(t, mock)
}

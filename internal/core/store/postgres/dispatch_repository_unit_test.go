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

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newDispatchRepoMock(t *testing.T) (*DispatchRepository, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return NewDispatchRepository(db), mock
}

// instanceColumns matches the SELECT / RETURNING column list used by the
// dispatch repository queries and scanInstanceSnapshot.
var instanceColumns = []string{
	"id", "run_id", "tenant_id", "job_id", "trigger_source",
	"status", "priority", "effective_priority", "partition_key",
	"idempotency_key", "idempotency_scope", "routing_key", "worker_id",
	"attempt", "max_attempt", "scheduled_at", "started_at",
	"finished_at", "lease_expires_at", "dispatched_at", "retry_at",
	"result_code", "error_msg", "trace_id", "created_at", "updated_at",
}

// addInstanceRow appends one instance row to the given sqlmock rows.
// Fields that are nullable use nil; timestamps use the provided now value.
func addInstanceRow(
	rows *sqlmock.Rows,
	id int64, runID, tenantID string, jobID int64,
	triggerSource, status string, priority int, scheduledAt, now time.Time,
) {
	rows.AddRow(
		id, runID, tenantID, jobID, triggerSource,
		status, priority, priority, nil, nil,         // effective_priority, partition_key, idempotency_key
		"job_instance_create", nil, nil,              // idempotency_scope, routing_key, worker_id
		1, 1,                                         // attempt, max_attempt
		scheduledAt, nil, nil,                         // scheduled_at, started_at, finished_at
		nil, nil, nil,                                 // lease_expires_at, dispatched_at, retry_at
		nil, nil, nil,                                 // result_code, error_msg, trace_id
		now, now,                                      // created_at, updated_at
	)
}

func makeClaimSpec(now time.Time) domaininstance.ClaimSpec {
	return domaininstance.ClaimSpec{
		TenantID:       "tenant-a",
		WorkerID:       "worker-1",
		LeaseExpiresAt: now.Add(30 * time.Second),
		Now:            now,
	}
}

func expectClaimNoCandidate(mock sqlmock.Sqlmock, spec domaininstance.ClaimSpec) {
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").
		WithArgs(spec.TenantID, spec.Now).
		WillReturnRows(sqlmock.NewRows(instanceColumns))
}

func expectClaimOneCandidate(mock sqlmock.Sqlmock, spec domaininstance.ClaimSpec, id int64, now time.Time) {
	rows := sqlmock.NewRows(instanceColumns)
	addInstanceRow(rows, id, "run-1", spec.TenantID, 101, "schedule", "pending", 5, now.Add(-time.Minute), now)
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").
		WithArgs(spec.TenantID, spec.Now).
		WillReturnRows(rows)
}

func expectPolicyLookup(mock sqlmock.Sqlmock, tenantID string, jobID int64, policy string) {
	mock.ExpectQuery("SELECT concurrency_policy FROM jobs").
		WithArgs(tenantID, jobID).
		WillReturnRows(sqlmock.NewRows([]string{"concurrency_policy"}).AddRow(policy))
}

func expectRunningCount(mock sqlmock.Sqlmock, tenantID string, jobID int64, count int) {
	mock.ExpectQuery("SELECT count").
		WithArgs(tenantID, jobID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
}

func expectUpdateToDispatched(mock sqlmock.Sqlmock, now time.Time, instanceID int64) {
	rows := sqlmock.NewRows(instanceColumns)
	addInstanceRow(rows, instanceID, "run-1", "tenant-a", 101, "schedule", "dispatched", 5, now.Add(-time.Minute), now)
	mock.ExpectQuery("UPDATE job_instances").
		WithArgs(now, now.Add(30*time.Second), instanceID).
		WillReturnRows(rows)
}

func expectCancelRunning(mock sqlmock.Sqlmock, tenantID string, jobID int64, now time.Time) {
	mock.ExpectExec("UPDATE job_instances").
		WithArgs(tenantID, jobID, now).
		WillReturnResult(sqlmock.NewResult(0, 2))
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestDispatchOne_NoCandidate(t *testing.T) {
	repo, mock := newDispatchRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	mock.ExpectBegin()
	expectClaimNoCandidate(mock, spec)
	mock.ExpectRollback()

	snap, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err != nil {
		t.Fatalf("DispatchOne() error = %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	if snap.ID != 0 {
		t.Fatalf("expected zero snapshot")
	}
	assertMock(t, mock)
}

func TestDispatchOne_DispatchAction(t *testing.T) {
	repo, mock := newDispatchRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	mock.ExpectBegin()
	expectClaimOneCandidate(mock, spec, 1, now)
	expectPolicyLookup(mock, spec.TenantID, 101, "allow")
	expectRunningCount(mock, spec.TenantID, 101, 0)
	expectUpdateToDispatched(mock, now, 1)
	mock.ExpectCommit()

	snap, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err != nil {
		t.Fatalf("DispatchOne() error = %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if snap.ID != 1 {
		t.Fatalf("expected id=1, got %d", snap.ID)
	}
	if snap.Status != "dispatched" {
		t.Fatalf("expected status=dispatched, got %q", snap.Status)
	}
	assertMock(t, mock)
}

func TestDispatchOne_SkipAction(t *testing.T) {
	repo, mock := newDispatchRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	mock.ExpectBegin()
	expectClaimOneCandidate(mock, spec, 1, now)
	expectPolicyLookup(mock, spec.TenantID, 101, "forbid")
	expectRunningCount(mock, spec.TenantID, 101, 1)
	mock.ExpectRollback()

	snap, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err != nil {
		t.Fatalf("DispatchOne() error = %v", err)
	}
	if found {
		t.Fatalf("expected found=false for skip action")
	}
	if snap.ID != 0 {
		t.Fatalf("expected zero snapshot for skip action")
	}
	assertMock(t, mock)
}

func TestDispatchOne_ReplaceAction(t *testing.T) {
	repo, mock := newDispatchRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	mock.ExpectBegin()
	expectClaimOneCandidate(mock, spec, 3, now)
	expectPolicyLookup(mock, spec.TenantID, 101, "replace")
	expectRunningCount(mock, spec.TenantID, 101, 2)
	expectCancelRunning(mock, spec.TenantID, 101, now)
	expectUpdateToDispatched(mock, now, 3)
	mock.ExpectCommit()

	snap, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err != nil {
		t.Fatalf("DispatchOne() error = %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if snap.ID != 3 {
		t.Fatalf("expected id=3, got %d", snap.ID)
	}
	if snap.Status != "dispatched" {
		t.Fatalf("expected status=dispatched, got %q", snap.Status)
	}
	assertMock(t, mock)
}

func TestDispatchOne_BeginTxError(t *testing.T) {
	repo, mock := newDispatchRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	mock.ExpectBegin().WillReturnError(errors.New("boom"))

	_, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err == nil || !strings.Contains(err.Error(), "begin dispatch tx") {
		t.Fatalf("expected begin dispatch tx error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestDispatchOne_ClaimError(t *testing.T) {
	repo, mock := newDispatchRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").
		WithArgs(spec.TenantID, spec.Now).
		WillReturnError(errors.New("claim boom"))
	mock.ExpectRollback()

	_, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err == nil || !strings.Contains(err.Error(), "claim dispatch candidate") {
		t.Fatalf("expected claim dispatch candidate error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestDispatchOne_PolicyLookupError(t *testing.T) {
	repo, mock := newDispatchRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	mock.ExpectBegin()
	expectClaimOneCandidate(mock, spec, 1, now)
	mock.ExpectQuery("SELECT concurrency_policy FROM jobs").
		WithArgs(spec.TenantID, int64(101)).
		WillReturnError(errors.New("policy boom"))
	mock.ExpectRollback()

	_, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err == nil || !strings.Contains(err.Error(), "lookup concurrency policy") {
		t.Fatalf("expected lookup concurrency policy error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestDispatchOne_RunningCountError(t *testing.T) {
	repo, mock := newDispatchRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	mock.ExpectBegin()
	expectClaimOneCandidate(mock, spec, 1, now)
	expectPolicyLookup(mock, spec.TenantID, 101, "allow")
	mock.ExpectQuery("SELECT count").
		WithArgs(spec.TenantID, int64(101)).
		WillReturnError(errors.New("count boom"))
	mock.ExpectRollback()

	_, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err == nil || !strings.Contains(err.Error(), "count running instances") {
		t.Fatalf("expected count running instances error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestDispatchOne_CommitError(t *testing.T) {
	repo, mock := newDispatchRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	mock.ExpectBegin()
	expectClaimOneCandidate(mock, spec, 1, now)
	expectPolicyLookup(mock, spec.TenantID, 101, "allow")
	expectRunningCount(mock, spec.TenantID, 101, 0)
	expectUpdateToDispatched(mock, now, 1)
	mock.ExpectCommit().WillReturnError(errors.New("commit boom"))

	_, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err == nil || !strings.Contains(err.Error(), "commit dispatch tx") {
		t.Fatalf("expected commit dispatch tx error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestDispatchOne_SkipRollbackError(t *testing.T) {
	repo, mock := newDispatchRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	mock.ExpectBegin()
	expectClaimOneCandidate(mock, spec, 1, now)
	expectPolicyLookup(mock, spec.TenantID, 101, "forbid")
	expectRunningCount(mock, spec.TenantID, 101, 1)
	mock.ExpectRollback().WillReturnError(errors.New("rb boom"))

	_, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err == nil || !strings.Contains(err.Error(), "rollback skip dispatch tx") {
		t.Fatalf("expected rollback skip dispatch tx error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestDispatchOne_CancelRunningError(t *testing.T) {
	repo, mock := newDispatchRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	mock.ExpectBegin()
	expectClaimOneCandidate(mock, spec, 1, now)
	expectPolicyLookup(mock, spec.TenantID, 101, "replace")
	expectRunningCount(mock, spec.TenantID, 101, 1)
	mock.ExpectExec("UPDATE job_instances").
		WithArgs(spec.TenantID, int64(101), now).
		WillReturnError(errors.New("cancel boom"))
	mock.ExpectRollback()

	_, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err == nil || !strings.Contains(err.Error(), "cancel running instances") {
		t.Fatalf("expected cancel running instances error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestDispatchOne_UpdateToDispatchingError(t *testing.T) {
	repo, mock := newDispatchRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	mock.ExpectBegin()
	expectClaimOneCandidate(mock, spec, 1, now)
	expectPolicyLookup(mock, spec.TenantID, 101, "allow")
	expectRunningCount(mock, spec.TenantID, 101, 0)
	mock.ExpectQuery("UPDATE job_instances").
		WithArgs(now, spec.LeaseExpiresAt, int64(1)).
		WillReturnError(errors.New("update boom"))
	mock.ExpectRollback()

	_, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err == nil {
		t.Fatalf("expected error from updateInstanceToDispatching")
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestDispatchOne_UnknownAction(t *testing.T) {
	repo, mock := newDispatchRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	mock.ExpectBegin()
	expectClaimOneCandidate(mock, spec, 1, now)
	expectPolicyLookup(mock, spec.TenantID, 101, "allow")
	expectRunningCount(mock, spec.TenantID, 101, 0)
	// Return unknown action from decide
	mock.ExpectRollback()

	_, found, err := repo.DispatchOne(context.Background(), spec, func(domaininstance.DispatchInput) domaininstance.DispatchDecision {
		return domaininstance.DispatchDecision{Action: "unknown_action"}
	})
	if err == nil || !strings.Contains(err.Error(), "unknown dispatch action") {
		t.Fatalf("expected unknown dispatch action error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestDispatchOne_NoCandidateRollbackError(t *testing.T) {
	repo, mock := newDispatchRepoMock(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	mock.ExpectBegin()
	expectClaimNoCandidate(mock, spec)
	mock.ExpectRollback().WillReturnError(errors.New("rb boom"))

	_, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err == nil || !strings.Contains(err.Error(), "rollback empty dispatch tx") {
		t.Fatalf("expected rollback empty dispatch tx error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestDispatchOne_DecideRequired(t *testing.T) {
	repo := NewDispatchRepository(nil)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	spec := makeClaimSpec(now)

	_, found, err := repo.DispatchOne(context.Background(), spec, nil)
	if err == nil || !strings.Contains(err.Error(), "decide policy is required") {
		t.Fatalf("expected decide policy is required error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
}

func TestNewDispatchRepository(t *testing.T) {
	db := &sql.DB{}
	repo := NewDispatchRepository(db)
	if repo == nil {
		t.Fatalf("expected repo != nil")
	}
	if repo.db != db {
		t.Fatalf("expected repository to keep db reference")
	}
}

func TestRecoverLeaseOrphans_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewDispatchRepository(db)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE job_instances").
		WithArgs(now).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec("UPDATE job_instances").
		WithArgs(now).
		WillReturnResult(sqlmock.NewResult(0, 0))

	n, _, err := repo.RecoverLeaseOrphans(context.Background(), now)
	if err != nil {
		t.Fatalf("RecoverLeaseOrphans() error = %v", err)
	}
	if n != 3 {
		t.Fatalf("expected n=3, got %d", n)
	}
}

func TestRecoverLeaseOrphans_Error(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewDispatchRepository(db)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE job_instances").
		WithArgs(now).
		WillReturnError(errors.New("recover boom"))

	_, _, err = repo.RecoverLeaseOrphans(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "recover dispatched") {
		t.Fatalf("expected recover dispatched orphans error, got %v", err)
	}
}

func TestRecoverLeaseOrphans_NoOrphans(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewDispatchRepository(db)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE job_instances").
		WithArgs(now).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE job_instances").
		WithArgs(now).
		WillReturnResult(sqlmock.NewResult(0, 0))

	n, _, err := repo.RecoverLeaseOrphans(context.Background(), now)
	if err != nil {
		t.Fatalf("RecoverLeaseOrphans() error = %v", err)
	}
	if n != 0 {
		t.Fatalf("expected n=0, got %d", n)
	}
}

func TestRefreshEffectivePriority_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewDispatchRepository(db)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE job_instances").
		WithArgs(now).
		WillReturnResult(sqlmock.NewResult(0, 5))

	n, err := repo.RefreshEffectivePriority(context.Background(), now)
	if err != nil {
		t.Fatalf("RefreshEffectivePriority() error = %v", err)
	}
	if n != 5 {
		t.Fatalf("expected n=5, got %d", n)
	}
}

func TestRefreshEffectivePriority_Error(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewDispatchRepository(db)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE job_instances").
		WithArgs(now).
		WillReturnError(errors.New("refresh boom"))

	_, err = repo.RefreshEffectivePriority(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "refresh effective priority") {
		t.Fatalf("expected refresh effective priority error, got %v", err)
	}
}

func TestRecoverLeaseOrphans_RunningError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewDispatchRepository(db)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE job_instances").
		WithArgs(now).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE job_instances").
		WithArgs(now).
		WillReturnError(errors.New("running orphan boom"))

	_, _, err = repo.RecoverLeaseOrphans(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "recover running orphans") {
		t.Fatalf("expected recover running orphans error, got %v", err)
	}
}

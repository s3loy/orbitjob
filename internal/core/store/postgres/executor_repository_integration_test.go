//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
	"orbitjob/internal/platform/postgrestest"
)

// ---------------------------------------------------------------------------
// ClaimNextDispatched
// ---------------------------------------------------------------------------

func TestClaimNextDispatched_Integration_ClaimsDispatched(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewExecutorRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedExecutorJob(t, db, executorJobSeed{
		TenantID:    "tenant-exec-claim",
		Name:        "claim-job",
		HandlerType: "exec",
		Payload:     `{"command":"echo","args":["hello"]}`,
		TimeoutSec:  30,
	})
	seedDispatchedInstance(t, db, executorInstanceSeed{
		TenantID:    "tenant-exec-claim",
		JobID:       jobID,
		Priority:    5,
		ScheduledAt: now.Add(-time.Minute),
	})

	tasks, err := repo.ClaimNextDispatched(context.Background(),
		"tenant-exec-claim", "worker-1", 10,
		now.Add(30*time.Second), now,
	)
	if err != nil {
		t.Fatalf("ClaimNextDispatched() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.HandlerType != "exec" {
		t.Fatalf("expected handler_type=exec, got %q", task.HandlerType)
	}
	if task.TimeoutSec != 30 {
		t.Fatalf("expected timeout_sec=30, got %d", task.TimeoutSec)
	}
	if task.HandlerPayload["command"] != "echo" {
		t.Fatalf("expected command=echo, got %v", task.HandlerPayload["command"])
	}
}

func TestClaimNextDispatched_Integration_OnlyClaimsDispatchedStatus(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewExecutorRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedExecutorJob(t, db, executorJobSeed{
		TenantID:    "tenant-exec-status",
		Name:        "status-job",
		HandlerType: "exec",
		Payload:     `{"command":"echo"}`,
	})
	seedExecutorPending(t, db, executorInstanceSeed{
		TenantID:    "tenant-exec-status",
		JobID:       jobID,
		Priority:    5,
		ScheduledAt: now.Add(-time.Minute),
	})

	tasks, err := repo.ClaimNextDispatched(context.Background(),
		"tenant-exec-status", "worker-1", 10,
		now.Add(30*time.Second), now,
	)
	if err != nil {
		t.Fatalf("ClaimNextDispatched() error = %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks for non-dispatched instance, got %d", len(tasks))
	}
}

func TestClaimNextDispatched_Integration_TransitionsToRunning(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewExecutorRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedExecutorJob(t, db, executorJobSeed{
		TenantID:    "tenant-exec-trans",
		Name:        "trans-job",
		HandlerType: "exec",
		Payload:     `{"command":"echo"}`,
	})
	instanceID := seedDispatchedInstance(t, db, executorInstanceSeed{
		TenantID:    "tenant-exec-trans",
		JobID:       jobID,
		Priority:    5,
		ScheduledAt: now.Add(-time.Minute),
	})

	_, err := repo.ClaimNextDispatched(context.Background(),
		"tenant-exec-trans", "worker-1", 10,
		now.Add(30*time.Second), now,
	)
	if err != nil {
		t.Fatalf("ClaimNextDispatched() error = %v", err)
	}

	assertInstanceStatus(t, db, "tenant-exec-trans", instanceID, "running")
	assertInstanceStartedAt(t, db, "tenant-exec-trans", instanceID, now)
}

func TestClaimNextDispatched_Integration_AlreadyClaimedReturnsEmpty(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewExecutorRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedExecutorJob(t, db, executorJobSeed{
		TenantID:    "tenant-exec-empty",
		Name:        "empty-job",
		HandlerType: "exec",
		Payload:     `{"command":"echo"}`,
	})
	seedDispatchedInstance(t, db, executorInstanceSeed{
		TenantID:    "tenant-exec-empty",
		JobID:       jobID,
		Priority:    5,
		ScheduledAt: now.Add(-time.Minute),
	})

	tasks, err := repo.ClaimNextDispatched(context.Background(),
		"tenant-exec-empty", "worker-1", 10,
		now.Add(30*time.Second), now,
	)
	if err != nil {
		t.Fatalf("first ClaimNextDispatched() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task on first claim, got %d", len(tasks))
	}

	tasks, err = repo.ClaimNextDispatched(context.Background(),
		"tenant-exec-empty", "worker-2", 10,
		now.Add(60*time.Second), now,
	)
	if err != nil {
		t.Fatalf("second ClaimNextDispatched() error = %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks on second claim, got %d", len(tasks))
	}
}

// ---------------------------------------------------------------------------
// CompleteInstance
// ---------------------------------------------------------------------------

func TestCompleteInstance_Integration_Success(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewExecutorRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedExecutorJob(t, db, executorJobSeed{
		TenantID:    "tenant-exec-complete",
		Name:        "complete-job",
		HandlerType: "exec",
		Payload:     `{"command":"echo"}`,
	})
	instanceID := seedExecutorRunning(t, db, executorInstanceSeed{
		TenantID:    "tenant-exec-complete",
		JobID:       jobID,
		Priority:    5,
		ScheduledAt: now.Add(-time.Minute),
		WorkerID:    "worker-1",
		StartedAt:   now.Add(-30 * time.Second),
	})

	resultCode := "0"
	err := repo.CompleteInstance(context.Background(), domaininstance.CompleteSpec{
		TenantID:   "tenant-exec-complete",
		InstanceID: instanceID,
		WorkerID:   "worker-1",
		Status:     "success",
		ResultCode: &resultCode,
		FinishedAt: now,
	})
	if err != nil {
		t.Fatalf("CompleteInstance() error = %v", err)
	}
	assertInstanceStatus(t, db, "tenant-exec-complete", instanceID, "success")
}

func TestCompleteInstance_Integration_RetryWait(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewExecutorRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedExecutorJob(t, db, executorJobSeed{
		TenantID:    "tenant-exec-retry",
		Name:        "retry-job",
		HandlerType: "exec",
		Payload:     `{"command":"echo"}`,
	})
	instanceID := seedExecutorRunning(t, db, executorInstanceSeed{
		TenantID:    "tenant-exec-retry",
		JobID:       jobID,
		Priority:    5,
		ScheduledAt: now.Add(-time.Minute),
		WorkerID:    "worker-1",
		StartedAt:   now.Add(-30 * time.Second),
		Attempt:     1,
		MaxAttempt:  3,
	})

	resultCode := "1"
	errorMsg := "some error"
	retryAt := now.Add(10 * time.Second)
	err := repo.CompleteInstance(context.Background(), domaininstance.CompleteSpec{
		TenantID:   "tenant-exec-retry",
		InstanceID: instanceID,
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
	assertInstanceStatus(t, db, "tenant-exec-retry", instanceID, "retry_wait")
	assertInstanceRetryAt(t, db, "tenant-exec-retry", instanceID, retryAt)
}

func TestCompleteInstance_Integration_AlreadyCompleted(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewExecutorRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedExecutorJob(t, db, executorJobSeed{
		TenantID:    "tenant-exec-done",
		Name:        "done-job",
		HandlerType: "exec",
		Payload:     `{"command":"echo"}`,
	})
	instanceID := seedExecutorRunning(t, db, executorInstanceSeed{
		TenantID:    "tenant-exec-done",
		JobID:       jobID,
		Priority:    5,
		ScheduledAt: now.Add(-time.Minute),
		WorkerID:    "worker-1",
		StartedAt:   now.Add(-30 * time.Second),
	})

	spec := domaininstance.CompleteSpec{
		TenantID:   "tenant-exec-done",
		InstanceID: instanceID,
		WorkerID:   "worker-1",
		Status:     "success",
		FinishedAt: now,
	}

	if err := repo.CompleteInstance(context.Background(), spec); err != nil {
		t.Fatalf("first CompleteInstance() error = %v", err)
	}

	err := repo.CompleteInstance(context.Background(), spec)
	if !errors.Is(err, ErrInstanceNotClaimed) {
		t.Fatalf("expected ErrInstanceNotClaimed, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ExtendLease
// ---------------------------------------------------------------------------

func TestExtendLease_Integration_Success(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewExecutorRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedExecutorJob(t, db, executorJobSeed{
		TenantID:    "tenant-exec-lease",
		Name:        "lease-job",
		HandlerType: "exec",
		Payload:     `{"command":"echo"}`,
	})
	instanceID := seedExecutorRunning(t, db, executorInstanceSeed{
		TenantID:    "tenant-exec-lease",
		JobID:       jobID,
		Priority:    5,
		ScheduledAt: now.Add(-time.Minute),
		WorkerID:    "worker-1",
		StartedAt:   now.Add(-30 * time.Second),
	})

	newExpiry := now.Add(120 * time.Second)
	err := repo.ExtendLease(context.Background(), "tenant-exec-lease", instanceID, "worker-1", newExpiry)
	if err != nil {
		t.Fatalf("ExtendLease() error = %v", err)
	}
	assertInstanceLeaseExpiresAt(t, db, "tenant-exec-lease", instanceID, newExpiry)
}

// ---------------------------------------------------------------------------
// seed / assert helpers
// ---------------------------------------------------------------------------

type executorJobSeed struct {
	TenantID    string
	Name        string
	HandlerType string
	Payload     string
	TimeoutSec  int
}

func seedExecutorJob(t *testing.T, db *sql.DB, in executorJobSeed) int64 {
	t.Helper()

	timeoutSec := in.TimeoutSec
	if timeoutSec == 0 {
		timeoutSec = 60
	}
	payload := in.Payload
	if payload == "" {
		payload = "{}"
	}

	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO jobs (name, tenant_id, trigger_type, handler_type, handler_payload, timeout_sec)
		VALUES ($1, $2, 'manual', $3, $4::jsonb, $5)
		RETURNING id
	`, in.Name, in.TenantID, in.HandlerType, payload, timeoutSec).Scan(&id)
	if err != nil {
		t.Fatalf("seed executor job: %v", err)
	}
	return id
}

type executorInstanceSeed struct {
	TenantID    string
	JobID       int64
	Priority    int
	ScheduledAt time.Time
	WorkerID    string
	StartedAt   time.Time
	Attempt     int
	MaxAttempt  int
}

func seedDispatchedInstance(t *testing.T, db *sql.DB, in executorInstanceSeed) int64 {
	t.Helper()

	attempt := in.Attempt
	if attempt == 0 {
		attempt = 1
	}
	maxAttempt := in.MaxAttempt
	if maxAttempt == 0 {
		maxAttempt = 1
	}

	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO job_instances (tenant_id, job_id, status, priority, effective_priority,
		                          scheduled_at, dispatched_at, attempt, max_attempt)
		VALUES ($1, $2, 'dispatched', $3, $3, $4, $4, $5, $6)
		RETURNING id
	`, in.TenantID, in.JobID, in.Priority, in.ScheduledAt, attempt, maxAttempt).Scan(&id)
	if err != nil {
		t.Fatalf("seed dispatched instance: %v", err)
	}
	return id
}

func seedExecutorPending(t *testing.T, db *sql.DB, in executorInstanceSeed) int64 {
	t.Helper()

	attempt := in.Attempt
	if attempt == 0 {
		attempt = 1
	}
	maxAttempt := in.MaxAttempt
	if maxAttempt == 0 {
		maxAttempt = 1
	}

	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO job_instances (tenant_id, job_id, status, priority, scheduled_at, attempt, max_attempt)
		VALUES ($1, $2, 'pending', $3, $4, $5, $6)
		RETURNING id
	`, in.TenantID, in.JobID, in.Priority, in.ScheduledAt, attempt, maxAttempt).Scan(&id)
	if err != nil {
		t.Fatalf("seed executor pending: %v", err)
	}
	return id
}

func seedExecutorRunning(t *testing.T, db *sql.DB, in executorInstanceSeed) int64 {
	t.Helper()

	attempt := in.Attempt
	if attempt == 0 {
		attempt = 1
	}
	maxAttempt := in.MaxAttempt
	if maxAttempt == 0 {
		maxAttempt = 1
	}

	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO job_instances (tenant_id, job_id, status, priority, scheduled_at,
		                           worker_id, started_at, attempt, max_attempt)
		VALUES ($1, $2, 'running', $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, in.TenantID, in.JobID, in.Priority, in.ScheduledAt,
		in.WorkerID, in.StartedAt, attempt, maxAttempt).Scan(&id)
	if err != nil {
		t.Fatalf("seed executor running: %v", err)
	}
	return id
}

func assertInstanceStartedAt(t *testing.T, db *sql.DB, tenantID string, instanceID int64, expected time.Time) {
	t.Helper()

	var startedAt time.Time
	err := db.QueryRowContext(context.Background(), `
		SELECT started_at FROM job_instances WHERE tenant_id = $1 AND id = $2
	`, tenantID, instanceID).Scan(&startedAt)
	if err != nil {
		t.Fatalf("query started_at: %v", err)
	}
	if !startedAt.Truncate(time.Second).Equal(expected.Truncate(time.Second)) {
		t.Fatalf("expected started_at=%v, got %v", expected, startedAt)
	}
}

func assertInstanceRetryAt(t *testing.T, db *sql.DB, tenantID string, instanceID int64, expected time.Time) {
	t.Helper()

	var retryAt time.Time
	err := db.QueryRowContext(context.Background(), `
		SELECT retry_at FROM job_instances WHERE tenant_id = $1 AND id = $2
	`, tenantID, instanceID).Scan(&retryAt)
	if err != nil {
		t.Fatalf("query retry_at: %v", err)
	}
	if !retryAt.Truncate(time.Second).Equal(expected.Truncate(time.Second)) {
		t.Fatalf("expected retry_at=%v, got %v", expected, retryAt)
	}
}

func assertInstanceLeaseExpiresAt(t *testing.T, db *sql.DB, tenantID string, instanceID int64, expected time.Time) {
	t.Helper()

	var leaseExpiresAt time.Time
	err := db.QueryRowContext(context.Background(), `
		SELECT lease_expires_at FROM job_instances WHERE tenant_id = $1 AND id = $2
	`, tenantID, instanceID).Scan(&leaseExpiresAt)
	if err != nil {
		t.Fatalf("query lease_expires_at: %v", err)
	}
	if !leaseExpiresAt.Truncate(time.Second).Equal(expected.Truncate(time.Second)) {
		t.Fatalf("expected lease_expires_at=%v, got %v", expected, leaseExpiresAt)
	}
}

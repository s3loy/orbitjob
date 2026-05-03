//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
	"orbitjob/internal/platform/postgrestest"
)

func TestDispatchOne_AllowDispatchesPending(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewDispatchRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedDispatchJob(t, db, dispatchJobSeed{
		TenantID:          "tenant-dispatch-allow",
		Name:              "allow-job",
		Priority:          5,
		ConcurrencyPolicy: "allow",
	})
	seedPendingInstance(t, db, dispatchInstanceSeed{
		TenantID:    "tenant-dispatch-allow",
		JobID:       jobID,
		Priority:    5,
		ScheduledAt: now.Add(-time.Minute),
	})

	spec := domaininstance.ClaimSpec{
		TenantID:       "tenant-dispatch-allow",
		WorkerID:       "worker-1",
		LeaseExpiresAt: now.Add(30 * time.Second),
		Now:            now,
	}

	snap, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err != nil {
		t.Fatalf("DispatchOne() error = %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if snap.Status != "dispatched" {
		t.Fatalf("expected status=dispatched, got %q", snap.Status)
	}
	if snap.WorkerID == nil || *snap.WorkerID != "worker-1" {
		t.Fatalf("expected worker_id=worker-1, got %v", snap.WorkerID)
	}
	assertInstanceStatus(t, db, "tenant-dispatch-allow", snap.ID, "dispatched")
}

func TestDispatchOne_ForbidSkipsWhenRunning(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewDispatchRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedDispatchJob(t, db, dispatchJobSeed{
		TenantID:          "tenant-dispatch-forbid",
		Name:              "forbid-job",
		Priority:          5,
		ConcurrencyPolicy: "forbid",
	})
	// Seed a running instance for the same job.
	seedDispatchingInstance(t, db, dispatchInstanceSeed{
		TenantID:    "tenant-dispatch-forbid",
		JobID:       jobID,
		Priority:    5,
		ScheduledAt: now.Add(-2 * time.Minute),
		WorkerID:    "worker-other",
	})
	// Seed a pending instance that should be skipped.
	seedPendingInstance(t, db, dispatchInstanceSeed{
		TenantID:    "tenant-dispatch-forbid",
		JobID:       jobID,
		Priority:    5,
		ScheduledAt: now.Add(-time.Minute),
	})

	spec := domaininstance.ClaimSpec{
		TenantID:       "tenant-dispatch-forbid",
		WorkerID:       "worker-1",
		LeaseExpiresAt: now.Add(30 * time.Second),
		Now:            now,
	}

	snap, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err != nil {
		t.Fatalf("DispatchOne() error = %v", err)
	}
	if found {
		t.Fatalf("expected found=false (skip)")
	}
	if snap.ID != 0 {
		t.Fatalf("expected zero snapshot for skip")
	}
}

func TestDispatchOne_ReplaceCancelsExisting(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewDispatchRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedDispatchJob(t, db, dispatchJobSeed{
		TenantID:          "tenant-dispatch-replace",
		Name:              "replace-job",
		Priority:          5,
		ConcurrencyPolicy: "replace",
	})
	runningID := seedDispatchingInstance(t, db, dispatchInstanceSeed{
		TenantID:    "tenant-dispatch-replace",
		JobID:       jobID,
		Priority:    5,
		ScheduledAt: now.Add(-2 * time.Minute),
		WorkerID:    "worker-old",
	})
	seedPendingInstance(t, db, dispatchInstanceSeed{
		TenantID:    "tenant-dispatch-replace",
		JobID:       jobID,
		Priority:    5,
		ScheduledAt: now.Add(-time.Minute),
	})

	spec := domaininstance.ClaimSpec{
		TenantID:       "tenant-dispatch-replace",
		WorkerID:       "worker-new",
		LeaseExpiresAt: now.Add(30 * time.Second),
		Now:            now,
	}

	snap, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err != nil {
		t.Fatalf("DispatchOne() error = %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if snap.Status != "dispatched" {
		t.Fatalf("expected status=dispatched, got %q", snap.Status)
	}

	// Verify old running instance was canceled.
	assertInstanceStatus(t, db, "tenant-dispatch-replace", runningID, "canceled")
	// Verify new instance is dispatched.
	assertInstanceStatus(t, db, "tenant-dispatch-replace", snap.ID, "dispatched")
}

func TestDispatchOne_PriorityOrdering(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewDispatchRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedDispatchJob(t, db, dispatchJobSeed{
		TenantID:          "tenant-dispatch-priority",
		Name:              "priority-job",
		Priority:          1,
		ConcurrencyPolicy: "allow",
	})
	// Seed instances with different priorities; high priority should be claimed first.
	seedPendingInstance(t, db, dispatchInstanceSeed{
		TenantID:    "tenant-dispatch-priority",
		JobID:       jobID,
		Priority:    1,
		ScheduledAt: now.Add(-5 * time.Minute),
	})
	seedPendingInstance(t, db, dispatchInstanceSeed{
		TenantID:    "tenant-dispatch-priority",
		JobID:       jobID,
		Priority:    10,
		ScheduledAt: now.Add(-time.Minute),
	})

	spec := domaininstance.ClaimSpec{
		TenantID:       "tenant-dispatch-priority",
		WorkerID:       "worker-1",
		LeaseExpiresAt: now.Add(30 * time.Second),
		Now:            now,
	}

	snap, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err != nil {
		t.Fatalf("DispatchOne() error = %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if snap.Priority != 10 {
		t.Fatalf("expected priority=10 (highest), got %d", snap.Priority)
	}
}

func TestDispatchOne_RetryWaitEligible(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewDispatchRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedDispatchJob(t, db, dispatchJobSeed{
		TenantID:          "tenant-dispatch-retry",
		Name:              "retry-job",
		Priority:          5,
		ConcurrencyPolicy: "allow",
	})
	seedRetryWaitInstance(t, db, dispatchInstanceSeed{
		TenantID:    "tenant-dispatch-retry",
		JobID:       jobID,
		Priority:    5,
		ScheduledAt: now.Add(-5 * time.Minute),
		RetryAt:     now.Add(-time.Second), // retry_at <= now, eligible
		Attempt:     1,
		MaxAttempt:  3,
	})

	spec := domaininstance.ClaimSpec{
		TenantID:       "tenant-dispatch-retry",
		WorkerID:       "worker-1",
		LeaseExpiresAt: now.Add(30 * time.Second),
		Now:            now,
	}

	snap, found, err := repo.DispatchOne(context.Background(), spec, domaininstance.DecideDispatch)
	if err != nil {
		t.Fatalf("DispatchOne() error = %v", err)
	}
	if !found {
		t.Fatalf("expected found=true for retry_wait instance")
	}
	if snap.Status != "dispatched" {
		t.Fatalf("expected status=dispatched, got %q", snap.Status)
	}
	if snap.Attempt != 2 {
		t.Fatalf("expected attempt=2 (incremented from retry_wait), got %d", snap.Attempt)
	}
}

func TestDispatchOne_NoCandidateReturnsFalse(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewDispatchRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	// Seed a job but no instances.
	seedDispatchJob(t, db, dispatchJobSeed{
		TenantID:          "tenant-dispatch-empty",
		Name:              "empty-job",
		Priority:          5,
		ConcurrencyPolicy: "allow",
	})

	spec := domaininstance.ClaimSpec{
		TenantID:       "tenant-dispatch-empty",
		WorkerID:       "worker-1",
		LeaseExpiresAt: now.Add(30 * time.Second),
		Now:            now,
	}

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
}

// ---------------------------------------------------------------------------
// seed / assert helpers
// ---------------------------------------------------------------------------

type dispatchJobSeed struct {
	TenantID          string
	Name              string
	Priority          int
	ConcurrencyPolicy string
}

func seedDispatchJob(t *testing.T, db *sql.DB, in dispatchJobSeed) int64 {
	t.Helper()

	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO jobs (name, tenant_id, priority, trigger_type, handler_type, concurrency_policy)
		VALUES ($1, $2, $3, 'manual', 'worker', $4)
		RETURNING id
	`, in.Name, in.TenantID, in.Priority, in.ConcurrencyPolicy).Scan(&id)
	if err != nil {
		t.Fatalf("seed dispatch job: %v", err)
	}
	return id
}

type dispatchInstanceSeed struct {
	TenantID    string
	JobID       int64
	Priority    int
	ScheduledAt time.Time
	WorkerID    string
	RetryAt     time.Time
	Attempt     int
	MaxAttempt  int
}

func seedPendingInstance(t *testing.T, db *sql.DB, in dispatchInstanceSeed) int64 {
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
		t.Fatalf("seed pending instance: %v", err)
	}
	return id
}

func seedDispatchingInstance(t *testing.T, db *sql.DB, in dispatchInstanceSeed) int64 {
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
		INSERT INTO job_instances (tenant_id, job_id, status, priority, scheduled_at, worker_id, attempt, max_attempt)
		VALUES ($1, $2, 'dispatched', $3, $4, $5, $6, $7)
		RETURNING id
	`, in.TenantID, in.JobID, in.Priority, in.ScheduledAt, in.WorkerID, attempt, maxAttempt).Scan(&id)
	if err != nil {
		t.Fatalf("seed dispatching instance: %v", err)
	}
	return id
}

func seedRetryWaitInstance(t *testing.T, db *sql.DB, in dispatchInstanceSeed) int64 {
	t.Helper()

	attempt := in.Attempt
	if attempt == 0 {
		attempt = 1
	}
	maxAttempt := in.MaxAttempt
	if maxAttempt == 0 {
		maxAttempt = 1
	}

	// For retry_wait we need a finished_at to satisfy the constraint.
	finishedAt := in.ScheduledAt.Add(10 * time.Second)

	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO job_instances (tenant_id, job_id, status, priority, scheduled_at, attempt, max_attempt, retry_at, finished_at)
		VALUES ($1, $2, 'retry_wait', $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, in.TenantID, in.JobID, in.Priority, in.ScheduledAt, attempt, maxAttempt, in.RetryAt, finishedAt).Scan(&id)
	if err != nil {
		t.Fatalf("seed retry_wait instance: %v", err)
	}
	return id
}

func assertInstanceStatus(t *testing.T, db *sql.DB, tenantID string, instanceID int64, expected string) {
	t.Helper()

	var status string
	err := db.QueryRowContext(context.Background(), `
		SELECT status FROM job_instances
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, instanceID).Scan(&status)
	if err != nil {
		t.Fatalf("query instance status: %v", err)
	}
	if status != expected {
		t.Fatalf("expected status=%q for instance %d, got %q", expected, instanceID, status)
	}
}

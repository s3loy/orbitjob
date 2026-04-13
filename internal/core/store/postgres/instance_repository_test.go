//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/platform/postgrestest"
)

func TestInstanceRepository_Create(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewInstanceRepository(db)

	jobID := seedJob(t, db, seedJobInput{
		Name:        "instance-job",
		TenantID:    "tenant-instance-create",
		TriggerType: domainjob.TriggerTypeManual,
		Timezone:    "UTC",
		HandlerType: "worker",
	})

	partitionKey := "tenant-instance-create:video"
	idempotencyKey := "manual-1"
	routingKey := "video.high"
	traceID := "trace-create"
	spec, err := domaininstance.NormalizeCreate(domaininstance.CreateInput{
		TenantID:       "tenant-instance-create",
		JobID:          jobID,
		TriggerSource:  domaininstance.TriggerSourceManual,
		ScheduledAt:    time.Now().UTC().Truncate(time.Second),
		Priority:       6,
		PartitionKey:   &partitionKey,
		IdempotencyKey: &idempotencyKey,
		RoutingKey:     &routingKey,
		MaxAttempt:     3,
		TraceID:        &traceID,
	})
	if err != nil {
		t.Fatalf("NormalizeCreate() error = %v", err)
	}

	out, err := repo.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if out.ID <= 0 {
		t.Fatalf("expected ID > 0, got %d", out.ID)
	}
	if out.RunID == "" {
		t.Fatalf("expected run_id to be set")
	}
	if out.Status != domaininstance.StatusPending {
		t.Fatalf("expected status=%q, got %q", domaininstance.StatusPending, out.Status)
	}
	if out.Attempt != 1 {
		t.Fatalf("expected attempt=%d, got %d", 1, out.Attempt)
	}
	if out.MaxAttempt != 3 {
		t.Fatalf("expected max_attempt=%d, got %d", 3, out.MaxAttempt)
	}
	if out.PartitionKey == nil || *out.PartitionKey != partitionKey {
		t.Fatalf("expected partition_key=%q, got %+v", partitionKey, out.PartitionKey)
	}

	var (
		status             string
		priority           int
		storedPartitionKey sql.NullString
		storedRoutingKey   sql.NullString
	)
	err = db.QueryRowContext(context.Background(), `
		SELECT status, priority, partition_key, routing_key
		FROM job_instances
		WHERE tenant_id = $1 AND id = $2
	`, "tenant-instance-create", out.ID).Scan(
		&status,
		&priority,
		&storedPartitionKey,
		&storedRoutingKey,
	)
	if err != nil {
		t.Fatalf("reload job instance: %v", err)
	}
	if status != domaininstance.StatusPending {
		t.Fatalf("expected stored status=%q, got %q", domaininstance.StatusPending, status)
	}
	if priority != 6 {
		t.Fatalf("expected stored priority=%d, got %d", 6, priority)
	}
	if !storedPartitionKey.Valid || storedPartitionKey.String != partitionKey {
		t.Fatalf("expected stored partition_key=%q, got %+v", partitionKey, storedPartitionKey)
	}
	if !storedRoutingKey.Valid || storedRoutingKey.String != routingKey {
		t.Fatalf("expected stored routing_key=%q, got %+v", routingKey, storedRoutingKey)
	}
}

func TestInstanceRepository_ClaimNextRunnable(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewInstanceRepository(db)

	jobID := seedJob(t, db, seedJobInput{
		Name:        "claim-job",
		TenantID:    "tenant-instance-claim",
		TriggerType: domainjob.TriggerTypeManual,
		Timezone:    "UTC",
		HandlerType: "worker",
	})

	now := time.Now().UTC().Truncate(time.Second)
	lowPriorityID := insertTestInstance(t, db, testInstanceSeed{
		TenantID:      "tenant-instance-claim",
		JobID:         jobID,
		Status:        domaininstance.StatusPending,
		Priority:      1,
		ScheduledAt:   now.Add(time.Minute),
		MaxAttempt:    1,
		TriggerSource: domaininstance.TriggerSourceSchedule,
	})
	retryWaitID := insertTestInstance(t, db, testInstanceSeed{
		TenantID:      "tenant-instance-claim",
		JobID:         jobID,
		Status:        domaininstance.StatusRetryWait,
		Priority:      9,
		ScheduledAt:   now,
		Attempt:       1,
		MaxAttempt:    3,
		RetryAt:       timePtr(now.Add(-time.Minute)),
		FinishedAt:    timePtr(now.Add(-30 * time.Second)),
		ResultCode:    stringPtr("TEMP_FAIL"),
		ErrorMsg:      stringPtr("upstream timeout"),
		TriggerSource: domaininstance.TriggerSourceSchedule,
	})

	claimSpec, err := domaininstance.NormalizeClaim(domaininstance.ClaimInput{
		TenantID:       "tenant-instance-claim",
		WorkerID:       "worker-a",
		Now:            now,
		LeaseExpiresAt: now.Add(30 * time.Second),
	})
	if err != nil {
		t.Fatalf("NormalizeClaim() error = %v", err)
	}

	out, found, err := repo.ClaimNextRunnable(context.Background(), claimSpec)
	if err != nil {
		t.Fatalf("ClaimNextRunnable() error = %v", err)
	}
	if !found {
		t.Fatalf("expected runnable instance to be found")
	}
	if out.ID != retryWaitID {
		t.Fatalf("expected claimed id=%d, got %d", retryWaitID, out.ID)
	}
	if out.Status != domaininstance.StatusDispatching {
		t.Fatalf("expected status=%q, got %q", domaininstance.StatusDispatching, out.Status)
	}
	if out.WorkerID == nil || *out.WorkerID != "worker-a" {
		t.Fatalf("expected worker_id=%q, got %+v", "worker-a", out.WorkerID)
	}
	if out.Attempt != 2 {
		t.Fatalf("expected attempt=%d, got %d", 2, out.Attempt)
	}
	if out.FinishedAt != nil {
		t.Fatalf("expected finished_at to be cleared, got %v", out.FinishedAt)
	}
	if out.RetryAt != nil {
		t.Fatalf("expected retry_at to be cleared, got %v", out.RetryAt)
	}
	if out.ErrorMsg != nil {
		t.Fatalf("expected error_msg to be cleared, got %v", out.ErrorMsg)
	}

	var lowPriorityStatus string
	err = db.QueryRowContext(context.Background(), `
		SELECT status
		FROM job_instances
		WHERE tenant_id = $1 AND id = $2
	`, "tenant-instance-claim", lowPriorityID).Scan(&lowPriorityStatus)
	if err != nil {
		t.Fatalf("reload low priority instance: %v", err)
	}
	if lowPriorityStatus != domaininstance.StatusPending {
		t.Fatalf("expected low priority instance to stay pending, got %q", lowPriorityStatus)
	}
}

func TestInstanceRepository_ClaimNextRunnable_NoCandidate(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewInstanceRepository(db)

	jobID := seedJob(t, db, seedJobInput{
		Name:        "claim-empty-job",
		TenantID:    "tenant-empty",
		TriggerType: domainjob.TriggerTypeManual,
		Timezone:    "UTC",
		HandlerType: "worker",
	})
	now := time.Now().UTC().Truncate(time.Second)
	insertTestInstance(t, db, testInstanceSeed{
		TenantID:      "tenant-empty",
		JobID:         jobID,
		Status:        domaininstance.StatusRetryWait,
		Priority:      5,
		ScheduledAt:   now,
		Attempt:       3,
		MaxAttempt:    3,
		RetryAt:       timePtr(now.Add(-time.Minute)),
		FinishedAt:    timePtr(now.Add(-30 * time.Second)),
		TriggerSource: domaininstance.TriggerSourceSchedule,
	})

	spec, err := domaininstance.NormalizeClaim(domaininstance.ClaimInput{
		TenantID:       "tenant-empty",
		WorkerID:       "worker-a",
		Now:            now,
		LeaseExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("NormalizeClaim() error = %v", err)
	}

	_, found, err := repo.ClaimNextRunnable(context.Background(), spec)
	if err != nil {
		t.Fatalf("ClaimNextRunnable() error = %v", err)
	}
	if found {
		t.Fatalf("expected no instance to be claimed")
	}
}

type testInstanceSeed struct {
	TenantID      string
	JobID         int64
	Status        string
	Priority      int
	PartitionKey  *string
	ScheduledAt   time.Time
	Attempt       int
	MaxAttempt    int
	RetryAt       *time.Time
	FinishedAt    *time.Time
	ResultCode    *string
	ErrorMsg      *string
	TriggerSource string
}

func insertTestInstance(t *testing.T, db *sql.DB, in testInstanceSeed) int64 {
	t.Helper()

	attempt := in.Attempt
	if attempt == 0 {
		attempt = 1
	}
	maxAttempt := in.MaxAttempt
	if maxAttempt == 0 {
		maxAttempt = 1
	}
	triggerSource := in.TriggerSource
	if triggerSource == "" {
		triggerSource = domaininstance.TriggerSourceSchedule
	}

	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO job_instances (
			tenant_id,
			job_id,
			trigger_source,
			scheduled_at,
			status,
			priority,
			partition_key,
			attempt,
			max_attempt,
			retry_at,
			finished_at,
			result_code,
			error_msg
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id
	`,
		in.TenantID,
		in.JobID,
		triggerSource,
		in.ScheduledAt,
		in.Status,
		in.Priority,
		in.PartitionKey,
		attempt,
		maxAttempt,
		in.RetryAt,
		in.FinishedAt,
		in.ResultCode,
		in.ErrorMsg,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert test instance: %v", err)
	}

	return id
}

func timePtr(in time.Time) *time.Time {
	return &in
}

func stringPtr(in string) *string {
	return &in
}

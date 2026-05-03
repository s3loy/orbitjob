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

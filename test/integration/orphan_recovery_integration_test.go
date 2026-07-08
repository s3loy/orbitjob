//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/postgrestest"
)

func TestMain(m *testing.M) {
	os.Exit(postgrestest.Run(m))
}

func TestRecoverLeaseOrphans_Integration_RecoveredInstanceCanBeCanceled(t *testing.T) {
	db := postgrestest.Open(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	var jobID int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO jobs (name, tenant_id, trigger_type, handler_type, handler_payload, retry_backoff_sec)
		VALUES ($1, $2, 'manual', 'http', '{}'::jsonb, 0)
		RETURNING id
	`, "orphan-cancel-job", "tenant-orphan-cancel").Scan(&jobID)
	if err != nil {
		t.Fatalf("seed job: %v", err)
	}

	var instanceID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO job_instances (
			tenant_id, job_id, status, priority, effective_priority, scheduled_at,
			worker_id, started_at, attempt, max_attempt, lease_expires_at
		)
		VALUES ($1, $2, 'running', 5, 5, $3, 'worker-orphan', $4, 1, 3, $5)
		RETURNING id
	`,
		"tenant-orphan-cancel", jobID,
		now.Add(-time.Minute),
		now.Add(-30*time.Second),
		now.Add(-time.Minute),
	).Scan(&instanceID)
	if err != nil {
		t.Fatalf("seed running instance: %v", err)
	}

	var runID string
	var version int
	err = db.QueryRowContext(ctx, `
		SELECT run_id::text, version FROM job_instances WHERE id = $1
	`, instanceID).Scan(&runID, &version)
	if err != nil {
		t.Fatalf("query initial instance: %v", err)
	}
	if version != 1 {
		t.Fatalf("expected initial version=1, got %d", version)
	}

	dispatchRepo := postgres.NewDispatchRepository(db)
	recoveredDispatched, recoveredRunning, err := dispatchRepo.RecoverLeaseOrphans(ctx, now)
	if err != nil {
		t.Fatalf("RecoverLeaseOrphans() error = %v", err)
	}
	if recoveredDispatched != 0 {
		t.Fatalf("expected recoveredDispatched=0, got %d", recoveredDispatched)
	}
	if recoveredRunning != 1 {
		t.Fatalf("expected recoveredRunning=1, got %d", recoveredRunning)
	}

	var status string
	err = db.QueryRowContext(ctx, `
		SELECT status, version FROM job_instances WHERE id = $1
	`, instanceID).Scan(&status, &version)
	if err != nil {
		t.Fatalf("query recovered instance: %v", err)
	}
	if status != "retry_wait" {
		t.Fatalf("expected status=retry_wait after recovery, got %q", status)
	}
	if version != 2 {
		t.Fatalf("expected version=2 after recovery, got %d", version)
	}

	instanceRepo := postgres.NewInstanceRepository(db)
	snap, err := instanceRepo.Cancel(ctx, runID, version)
	if err != nil {
		t.Fatalf("Cancel() after recovery error = %v", err)
	}
	if snap.Status != "canceled" {
		t.Fatalf("expected canceled status, got %q", snap.Status)
	}
	if snap.Version != 3 {
		t.Fatalf("expected version=3 after cancel, got %d", snap.Version)
	}
}

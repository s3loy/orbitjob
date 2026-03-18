package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"orbitjob/internal/job"
)

// TestJobRepository_Create is an integration test against a real PostgreSQL
// instance. It verifies the main insert path for a cron-triggered job.
func TestJobRepository_Create(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	db, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)

	// a normal cron job creation flow
	cron := "*/5 * * * *"
	in := job.CreateJobRequest{
		Name:        "demo-job",
		TenantID:    "default",
		TriggerType: "cron",
		CronExpr:    &cron,
		Timezone:    "UTC",
		HandlerType: "http",
		HandlerPayload: map[string]any{
			"url": "https://example.com/hook",
		},
	}

	input := job.NewCreateJobInput(in)
	spec, err := job.NormalizeCreateJob(time.Now().UTC(), input)
	if err != nil {
		t.Fatalf("NormalizeCreateJob() error = %v", err)
	}

	out, err := repo.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if out.ID <= 0 {
		t.Fatalf("expected ID > 0, got %d", out.ID)
	}
	if out.Name != in.Name {
		t.Fatalf("expected name=%q, got %q", in.Name, out.Name)
	}
	if out.TenantID != in.TenantID {
		t.Fatalf("expected tenant_id=%q, got %q", in.TenantID, out.TenantID)
	}
	if out.Status != "active" {
		t.Fatalf("expected sttus=active, got %q", out.Status)
	}
	if out.NextRunAt == nil {
		t.Fatalf("expected next_run_at to be set for cron job")
	}
}

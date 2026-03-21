package postgres

import (
	"context"
	"fmt"
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
	input := job.CreateJobInput{
		Name:        "demo-job",
		TenantID:    "default",
		TriggerType: job.TriggerTypeCron,
		CronExpr:    &cron,
		Timezone:    "UTC",
		HandlerType: "http",
		HandlerPayload: map[string]any{
			"url": "https://example.com/hook",
		},
	}

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
	if out.Name != input.Name {
		t.Fatalf("expected name=%q, got %q", input.Name, out.Name)
	}
	if out.TenantID != input.TenantID {
		t.Fatalf("expected tenant_id=%q, got %q", input.TenantID, out.TenantID)
	}
	if out.Status != "active" {
		t.Fatalf("expected status=active, got %q", out.Status)
	}
	if out.NextRunAt == nil {
		t.Fatalf("expected next_run_at to be set for cron job")
	}
}

func TestJobRepository_List(t *testing.T) {
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
	ctx := context.Background()

	now := time.Now().UTC()
	tenantID := fmt.Sprintf("tenant-list-%d", now.UnixNano())

	cron := "*/10 * * * *"
	activeInput := job.CreateJobInput{
		Name:        fmt.Sprintf("active-job-%d", now.UnixNano()),
		TenantID:    tenantID,
		TriggerType: job.TriggerTypeCron,
		CronExpr:    &cron,
		Timezone:    "Asia/Shanghai",
		HandlerType: "http",
	}
	activeSpec, err := job.NormalizeCreateJob(now, activeInput)
	if err != nil {
		t.Fatalf("NormalizeCreateJob(active) error = %v", err)
	}
	activeJob, err := repo.Create(ctx, activeSpec)
	if err != nil {
		t.Fatalf("Create(active) error = %v", err)
	}

	pausedInput := job.CreateJobInput{
		Name:        fmt.Sprintf("paused-job-%d", now.UnixNano()),
		TenantID:    tenantID,
		TriggerType: job.TriggerTypeManual,
		HandlerType: "http",
	}
	pausedSpec, err := job.NormalizeCreateJob(now, pausedInput)
	if err != nil {
		t.Fatalf("NormalizeCreateJob(paused) error = %v", err)
	}
	pausedJob, err := repo.Create(ctx, pausedSpec)
	if err != nil {
		t.Fatalf("Create(paused) error = %v", err)
	}

	if _, err := db.ExecContext(ctx, `
                UPDATE jobs
                SET status = 'paused'
                WHERE id = $1
        `, pausedJob.ID); err != nil {
		t.Fatalf("pause job: %v", err)
	}

	allItems, err := repo.List(ctx, job.ListJobsQuery{
		TenantID: tenantID,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("List(all) error = %v", err)
	}
	if len(allItems) != 2 {
		t.Fatalf("expected 2 items, got %d", len(allItems))
	}

	if allItems[0].ID != pausedJob.ID {
		t.Fatalf("expected newest item id=%d, got %d", pausedJob.ID, allItems[0].ID)
	}
	if allItems[0].Status != job.JobStatusPaused {
		t.Fatalf("expected paused status, got %q", allItems[0].Status)
	}
	if allItems[0].ScheduleSummary != "manual" {
		t.Fatalf("expected manual summary, got %q", allItems[0].ScheduleSummary)
	}

	if allItems[1].ID != activeJob.ID {
		t.Fatalf("expected second item id=%d, got %d", activeJob.ID, allItems[1].ID)
	}
	if allItems[1].Status != job.JobStatusActive {
		t.Fatalf("expected active status, got %q", allItems[1].Status)
	}
	if allItems[1].ScheduleSummary != "cron: */10 * * * * (Asia/Shanghai)" {
		t.Fatalf("unexpected schedule summary: %q", allItems[1].ScheduleSummary)
	}
	if allItems[1].HandlerType != "http" {
		t.Fatalf("expected handler_type=%q, got %q", "http", allItems[1].HandlerType)
	}
	if allItems[1].NextRunAt == nil {
		t.Fatalf("expected next_run_at to be set for cron job")
	}

	activeItems, err := repo.List(ctx, job.ListJobsQuery{
		TenantID: tenantID,
		Status:   job.JobStatusActive,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("List(active) error = %v", err)
	}
	if len(activeItems) != 1 {
		t.Fatalf("expected 1 active item, got %d", len(activeItems))
	}
	if activeItems[0].ID != activeJob.ID {
		t.Fatalf("expected active item id=%d, got %d", activeJob.ID, activeItems[0].ID)
	}
	if activeItems[0].LastScheduledAt != nil {
		t.Fatalf("expected last_scheduled_at to be nil, got %v",
			*activeItems[0].LastScheduledAt)
	}
}

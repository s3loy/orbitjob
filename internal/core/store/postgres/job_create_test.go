//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/platform/postgrestest"
)

func TestJobRepository_Create(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewJobRepository(db)

	now := time.Now().UTC()
	cron := "*/5 * * * *"
	input := domainjob.CreateInput{
		Name:        "demo-job",
		TenantID:    "default",
		TriggerType: domainjob.TriggerTypeCron,
		CronExpr:    &cron,
		Timezone:    "UTC",
		HandlerType: "http",
		HandlerPayload: map[string]any{
			"url": "https://example.com/hook",
		},
	}

	spec, err := domainjob.NormalizeCreate(now, input)
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
	if out.CreatedAt.IsZero() {
		t.Fatalf("expected created_at to be set")
	}
	if out.UpdatedAt.IsZero() {
		t.Fatalf("expected updated_at to be set")
	}
}

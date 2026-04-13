//go:build integration

package postgres

import (
	"context"
	"database/sql"
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
	partitionKey := "tenant-default:video"
	input := domainjob.CreateInput{
		Name:         "demo-job",
		TenantID:     "default",
		Priority:     4,
		PartitionKey: &partitionKey,
		TriggerType:  domainjob.TriggerTypeCron,
		CronExpr:     &cron,
		Timezone:     "UTC",
		HandlerType:  "http",
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
	if out.Version != 1 {
		t.Fatalf("expected version=1, got %d", out.Version)
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

	var (
		storedPriority     int
		storedPartitionKey sql.NullString
	)
	err = db.QueryRowContext(context.Background(), `
		SELECT priority, partition_key
		FROM jobs
		WHERE tenant_id = $1 AND id = $2
	`, input.TenantID, out.ID).Scan(&storedPriority, &storedPartitionKey)
	if err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if storedPriority != 4 {
		t.Fatalf("expected stored priority=%d, got %d", 4, storedPriority)
	}
	if !storedPartitionKey.Valid || storedPartitionKey.String != partitionKey {
		t.Fatalf("expected stored partition_key=%q, got %+v", partitionKey, storedPartitionKey)
	}
}

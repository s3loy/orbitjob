//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	query "orbitjob/internal/admin/app/job/query"
	"orbitjob/internal/platform/postgrestest"
)

func TestJobRepository_List(t *testing.T) {
	db := postgrestest.Open(t)
	readRepo := NewJobRepository(db)
	ctx := context.Background()

	now := time.Now().UTC()
	tenantID := fmt.Sprintf("tenant-list-%d", now.UnixNano())
	activeNextRunAt := now.Add(10 * time.Minute).Truncate(time.Second)

	activeJobID := insertTestJob(t, db, testJobSeed{
		Name:        fmt.Sprintf("active-job-%d", now.UnixNano()),
		TenantID:    tenantID,
		TriggerType: triggerTypeCron,
		CronExpr:    stringPtr("*/10 * * * *"),
		Timezone:    "Asia/Shanghai",
		HandlerType: "http",
		Status:      query.StatusActive,
		NextRunAt:   &activeNextRunAt,
	})
	pausedJobID := insertTestJob(t, db, testJobSeed{
		Name:        fmt.Sprintf("paused-job-%d", now.UnixNano()),
		TenantID:    tenantID,
		TriggerType: triggerTypeManual,
		Timezone:    "UTC",
		HandlerType: "http",
		Status:      query.StatusPaused,
	})

	allItems, err := readRepo.List(ctx, query.ListInput{
		TenantID: tenantID,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("List(all) error = %v", err)
	}
	if len(allItems) != 2 {
		t.Fatalf("expected 2 items, got %d", len(allItems))
	}

	if allItems[0].ID != pausedJobID {
		t.Fatalf("expected newest item id=%d, got %d", pausedJobID, allItems[0].ID)
	}
	if allItems[0].Status != query.StatusPaused {
		t.Fatalf("expected paused status, got %q", allItems[0].Status)
	}
	if allItems[0].ScheduleSummary != "manual" {
		t.Fatalf("expected manual summary, got %q", allItems[0].ScheduleSummary)
	}

	if allItems[1].ID != activeJobID {
		t.Fatalf("expected second item id=%d, got %d", activeJobID, allItems[1].ID)
	}
	if allItems[1].Status != query.StatusActive {
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

	activeItems, err := readRepo.List(ctx, query.ListInput{
		TenantID: tenantID,
		Status:   query.StatusActive,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("List(active) error = %v", err)
	}
	if len(activeItems) != 1 {
		t.Fatalf("expected 1 active item, got %d", len(activeItems))
	}
	if activeItems[0].ID != activeJobID {
		t.Fatalf("expected active item id=%d, got %d", activeJobID, activeItems[0].ID)
	}
	if activeItems[0].LastScheduledAt != nil {
		t.Fatalf("expected last_scheduled_at to be nil, got %v",
			*activeItems[0].LastScheduledAt)
	}
}

type testJobSeed struct {
	Name        string
	TenantID    string
	TriggerType string
	CronExpr    *string
	Timezone    string
	HandlerType string
	Status      string
	NextRunAt   *time.Time
}

const (
	triggerTypeCron   = "cron"
	triggerTypeManual = "manual"
)

func insertTestJob(t *testing.T, db *sql.DB, in testJobSeed) int64 {
	t.Helper()

	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO jobs (
			name,
			tenant_id,
			trigger_type,
			cron_expr,
			timezone,
			handler_type,
			status,
			next_run_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`,
		in.Name,
		in.TenantID,
		in.TriggerType,
		in.CronExpr,
		in.Timezone,
		in.HandlerType,
		in.Status,
		in.NextRunAt,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert test job: %v", err)
	}

	return id
}

func stringPtr(s string) *string {
	return &s
}

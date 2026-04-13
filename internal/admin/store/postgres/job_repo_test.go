//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	query "orbitjob/internal/admin/app/job/query"
	"orbitjob/internal/domain/resource"
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
		Name:         fmt.Sprintf("active-job-%d", now.UnixNano()),
		TenantID:     tenantID,
		Priority:     7,
		PartitionKey: stringPtr("tenant-a:etl"),
		TriggerType:  triggerTypeCron,
		CronExpr:     stringPtr("*/10 * * * *"),
		Timezone:     "Asia/Shanghai",
		HandlerType:  "http",
		Status:       query.StatusActive,
		NextRunAt:    &activeNextRunAt,
	})
	pausedJobID := insertTestJob(t, db, testJobSeed{
		Name:         fmt.Sprintf("paused-job-%d", now.UnixNano()),
		TenantID:     tenantID,
		Priority:     1,
		PartitionKey: stringPtr("tenant-a:ops"),
		TriggerType:  triggerTypeManual,
		Timezone:     "UTC",
		HandlerType:  "http",
		Status:       query.StatusPaused,
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
	if allItems[0].Priority != 1 {
		t.Fatalf("expected paused priority=%d, got %d", 1, allItems[0].Priority)
	}
	if allItems[0].PartitionKey == nil || *allItems[0].PartitionKey != "tenant-a:ops" {
		t.Fatalf("expected paused partition_key=%q, got %+v", "tenant-a:ops", allItems[0].PartitionKey)
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
	if allItems[1].Priority != 7 {
		t.Fatalf("expected active priority=%d, got %d", 7, allItems[1].Priority)
	}
	if allItems[1].PartitionKey == nil || *allItems[1].PartitionKey != "tenant-a:etl" {
		t.Fatalf("expected active partition_key=%q, got %+v", "tenant-a:etl", allItems[1].PartitionKey)
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

func TestJobRepository_Get(t *testing.T) {
	db := postgrestest.Open(t)
	readRepo := NewJobRepository(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	tenantID := fmt.Sprintf("tenant-get-%d", now.UnixNano())
	nextRunAt := now.Add(5 * time.Minute)
	lastScheduledAt := now.Add(-2 * time.Minute)

	jobID := insertTestJob(t, db, testJobSeed{
		Name:                 fmt.Sprintf("detail-job-%d", now.UnixNano()),
		TenantID:             tenantID,
		Priority:             11,
		PartitionKey:         stringPtr("tenant-a:reports"),
		TriggerType:          triggerTypeCron,
		CronExpr:             stringPtr("*/5 * * * *"),
		Timezone:             "Asia/Shanghai",
		HandlerType:          "http",
		HandlerPayload:       `{"url":"https://example.com/hook"}`,
		TimeoutSec:           120,
		RetryLimit:           3,
		RetryBackoffSec:      15,
		RetryBackoffStrategy: "exponential",
		ConcurrencyPolicy:    "forbid",
		MisfirePolicy:        "fire_now",
		Status:               query.StatusActive,
		NextRunAt:            &nextRunAt,
		LastScheduledAt:      &lastScheduledAt,
	})

	item, err := readRepo.Get(ctx, query.GetInput{
		ID:       jobID,
		TenantID: tenantID,
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if item.ID != jobID {
		t.Fatalf("expected id=%d, got %d", jobID, item.ID)
	}
	if item.Version != 1 {
		t.Fatalf("expected version=%d, got %d", 1, item.Version)
	}
	if item.Priority != 11 {
		t.Fatalf("expected priority=%d, got %d", 11, item.Priority)
	}
	if item.PartitionKey == nil || *item.PartitionKey != "tenant-a:reports" {
		t.Fatalf("expected partition_key=%q, got %+v", "tenant-a:reports", item.PartitionKey)
	}
	if item.CronExpr == nil || *item.CronExpr != "*/5 * * * *" {
		t.Fatalf("expected cron_expr to be loaded, got %v", item.CronExpr)
	}
	if item.ScheduleSummary != "cron: */5 * * * * (Asia/Shanghai)" {
		t.Fatalf("unexpected schedule summary: %q", item.ScheduleSummary)
	}
	if item.TimeoutSec != 120 {
		t.Fatalf("expected timeout_sec=%d, got %d", 120, item.TimeoutSec)
	}
	if item.RetryLimit != 3 {
		t.Fatalf("expected retry_limit=%d, got %d", 3, item.RetryLimit)
	}
	if item.RetryBackoffSec != 15 {
		t.Fatalf("expected retry_backoff_sec=%d, got %d", 15, item.RetryBackoffSec)
	}
	if item.RetryBackoffStrategy != "exponential" {
		t.Fatalf("expected retry_backoff_strategy=%q, got %q", "exponential", item.RetryBackoffStrategy)
	}
	if item.ConcurrencyPolicy != "forbid" {
		t.Fatalf("expected concurrency_policy=%q, got %q", "forbid", item.ConcurrencyPolicy)
	}
	if item.MisfirePolicy != "fire_now" {
		t.Fatalf("expected misfire_policy=%q, got %q", "fire_now", item.MisfirePolicy)
	}
	if item.HandlerPayload["url"] != "https://example.com/hook" {
		t.Fatalf("expected handler_payload.url to be loaded, got %#v", item.HandlerPayload["url"])
	}
	if item.NextRunAt == nil || !item.NextRunAt.Equal(nextRunAt) {
		t.Fatalf("expected next_run_at=%v, got %v", nextRunAt, item.NextRunAt)
	}
	if item.LastScheduledAt == nil || !item.LastScheduledAt.Equal(lastScheduledAt) {
		t.Fatalf("expected last_scheduled_at=%v, got %v", lastScheduledAt, item.LastScheduledAt)
	}
}

func TestJobRepository_GetHidesDeletedJob(t *testing.T) {
	db := postgrestest.Open(t)
	readRepo := NewJobRepository(db)
	ctx := context.Background()

	now := time.Now().UTC()
	tenantID := fmt.Sprintf("tenant-get-deleted-%d", now.UnixNano())
	jobID := insertTestJob(t, db, testJobSeed{
		Name:        fmt.Sprintf("deleted-job-%d", now.UnixNano()),
		TenantID:    tenantID,
		TriggerType: triggerTypeManual,
		Timezone:    "UTC",
		HandlerType: "http",
		Status:      query.StatusPaused,
	})

	_, err := db.ExecContext(ctx, `
		UPDATE jobs
		SET deleted_at = NOW()
		WHERE id = $1
	`, jobID)
	if err != nil {
		t.Fatalf("soft delete test job: %v", err)
	}

	_, err = readRepo.Get(ctx, query.GetInput{
		ID:       jobID,
		TenantID: tenantID,
	})
	if err == nil {
		t.Fatalf("expected not found error, got nil")
	}

	var notFoundErr *resource.NotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected NotFoundError, got %T (%v)", err, err)
	}
	if notFoundErr.Resource != "job" {
		t.Fatalf("expected resource=%q, got %q", "job", notFoundErr.Resource)
	}
}

type testJobSeed struct {
	Name                 string
	TenantID             string
	Priority             int
	PartitionKey         *string
	TriggerType          string
	CronExpr             *string
	Timezone             string
	HandlerType          string
	HandlerPayload       string
	TimeoutSec           int
	RetryLimit           int
	RetryBackoffSec      int
	RetryBackoffStrategy string
	ConcurrencyPolicy    string
	MisfirePolicy        string
	Status               string
	NextRunAt            *time.Time
	LastScheduledAt      *time.Time
}

const (
	triggerTypeCron   = "cron"
	triggerTypeManual = "manual"
)

func insertTestJob(t *testing.T, db *sql.DB, in testJobSeed) int64 {
	t.Helper()

	handlerPayload := in.HandlerPayload
	if handlerPayload == "" {
		handlerPayload = `{}`
	}

	timeoutSec := in.TimeoutSec
	if timeoutSec == 0 {
		timeoutSec = 60
	}

	retryBackoffStrategy := in.RetryBackoffStrategy
	if retryBackoffStrategy == "" {
		retryBackoffStrategy = "fixed"
	}

	concurrencyPolicy := in.ConcurrencyPolicy
	if concurrencyPolicy == "" {
		concurrencyPolicy = "allow"
	}

	misfirePolicy := in.MisfirePolicy
	if misfirePolicy == "" {
		misfirePolicy = "skip"
	}

	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO jobs (
			name,
			tenant_id,
			priority,
			partition_key,
			trigger_type,
			cron_expr,
			timezone,
			handler_type,
			handler_payload,
			timeout_sec,
			retry_limit,
			retry_backoff_sec,
			retry_backoff_strategy,
			concurrency_policy,
			misfire_policy,
			status,
			next_run_at,
			last_scheduled_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb,
			$10, $11, $12, $13, $14, $15, $16, $17, $18
		)
		RETURNING id
	`,
		in.Name,
		in.TenantID,
		in.Priority,
		in.PartitionKey,
		in.TriggerType,
		in.CronExpr,
		in.Timezone,
		in.HandlerType,
		handlerPayload,
		timeoutSec,
		in.RetryLimit,
		in.RetryBackoffSec,
		retryBackoffStrategy,
		concurrencyPolicy,
		misfirePolicy,
		in.Status,
		in.NextRunAt,
		in.LastScheduledAt,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert test job: %v", err)
	}

	return id
}

func stringPtr(s string) *string {
	return &s
}

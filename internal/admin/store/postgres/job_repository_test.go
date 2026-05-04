package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	query "orbitjob/internal/admin/app/job/query"
	"orbitjob/internal/domain/resource"
)

func TestNewJobRepository(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)
	if repo == nil {
		t.Fatal("expected non-nil repository")
	}
	if repo.db != db {
		t.Fatal("expected db to be set")
	}
}

func TestJobRepository_Get_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{
		"id", "name", "tenant_id", "version", "priority",
		"trigger_type", "partition_key", "cron_expr", "timezone",
		"handler_type", "handler_payload",
		"timeout_sec", "retry_limit", "retry_backoff_sec", "retry_backoff_strategy",
		"concurrency_policy", "misfire_policy", "status",
		"next_run_at", "last_scheduled_at", "created_at", "updated_at",
	}).AddRow(
		42, "demo-job", "default", 1, 5,
		"cron", "tenant-a:batch", "*/5 * * * *", "UTC",
		"http", []byte(`{"url":"https://example.com"}`),
		120, 3, 10, "exponential",
		"forbid", "fire_now", "active",
		now, now, now, now,
	)

	mock.ExpectQuery(`SELECT (.+) FROM jobs WHERE tenant_id = \$1 AND id = \$2 AND deleted_at IS NULL`).
		WithArgs("default", int64(42)).
		WillReturnRows(rows)

	repo := NewJobRepository(db)
	item, err := repo.Get(context.Background(), query.GetInput{ID: 42, TenantID: "default"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if item.ID != 42 || item.Name != "demo-job" {
		t.Fatalf("unexpected item: %+v", item)
	}
}

func TestJobRepository_Get_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT (.+) FROM jobs WHERE tenant_id = \$1 AND id = \$2 AND deleted_at IS NULL`).
		WithArgs("default", int64(42)).
		WillReturnError(sql.ErrNoRows)

	repo := NewJobRepository(db)
	_, err = repo.Get(context.Background(), query.GetInput{ID: 42, TenantID: "default"})
	if err == nil {
		t.Fatal("expected error")
	}

	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestJobRepository_Get_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT (.+) FROM jobs WHERE tenant_id = \$1 AND id = \$2 AND deleted_at IS NULL`).
		WithArgs("default", int64(42)).
		WillReturnError(errors.New("connection refused"))

	repo := NewJobRepository(db)
	_, err = repo.Get(context.Background(), query.GetInput{ID: 42, TenantID: "default"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestJobRepository_List_AllStatuses(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{
		"id", "name", "tenant_id", "priority",
		"trigger_type", "partition_key", "cron_expr", "timezone",
		"handler_type", "concurrency_policy", "misfire_policy", "status",
		"next_run_at", "last_scheduled_at", "created_at", "updated_at",
	}).AddRow(
		1, "job-1", "default", 3,
		"manual", nil, nil, "UTC",
		"http", "allow", "skip", "active",
		nil, nil, now, now,
	).AddRow(
		2, "job-2", "default", 7,
		"cron", "tenant-a:batch", "0 */2 * * *", "Asia/Shanghai",
		"exec", "forbid", "fire_now", "active",
		now, now, now, now,
	)

	mock.ExpectQuery(`SELECT (.+) FROM jobs WHERE tenant_id = \$1 AND deleted_at IS NULL ORDER BY id DESC LIMIT \$2 OFFSET \$3`).
		WithArgs("default", 50, 0).
		WillReturnRows(rows)

	repo := NewJobRepository(db)
	items, err := repo.List(context.Background(), query.ListInput{
		TenantID: "default",
		Limit:    50,
		Offset:   0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestJobRepository_List_FilterByStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{
		"id", "name", "tenant_id", "priority",
		"trigger_type", "partition_key", "cron_expr", "timezone",
		"handler_type", "concurrency_policy", "misfire_policy", "status",
		"next_run_at", "last_scheduled_at", "created_at", "updated_at",
	}).AddRow(
		1, "paused-job", "default", 5,
		"manual", nil, nil, "UTC",
		"http", "allow", "skip", "paused",
		nil, nil, now, now,
	)

	mock.ExpectQuery(`SELECT (.+) FROM jobs WHERE tenant_id = \$1 AND deleted_at IS NULL AND status = \$2 ORDER BY id DESC LIMIT \$3 OFFSET \$4`).
		WithArgs("default", "paused", 50, 0).
		WillReturnRows(rows)

	repo := NewJobRepository(db)
	items, err := repo.List(context.Background(), query.ListInput{
		TenantID: "default",
		Status:   "paused",
		Limit:    50,
		Offset:   0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Status != "paused" {
		t.Fatalf("expected status=paused, got %s", items[0].Status)
	}
}

func TestJobRepository_List_Empty(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	rows := sqlmock.NewRows([]string{
		"id", "name", "tenant_id", "priority",
		"trigger_type", "partition_key", "cron_expr", "timezone",
		"handler_type", "concurrency_policy", "misfire_policy", "status",
		"next_run_at", "last_scheduled_at", "created_at", "updated_at",
	})

	mock.ExpectQuery(`SELECT (.+) FROM jobs WHERE tenant_id = \$1 AND deleted_at IS NULL`).
		WithArgs("default", 50, 0).
		WillReturnRows(rows)

	repo := NewJobRepository(db)
	items, err := repo.List(context.Background(), query.ListInput{
		TenantID: "default",
		Limit:    50,
		Offset:   0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}

func TestJobRepository_List_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT (.+) FROM jobs`).
		WithArgs("default", 50, 0).
		WillReturnError(errors.New("connection refused"))

	repo := NewJobRepository(db)
	_, err = repo.List(context.Background(), query.ListInput{
		TenantID: "default",
		Limit:    50,
		Offset:   0,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

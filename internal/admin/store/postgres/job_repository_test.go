package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	jobquery "orbitjob/internal/admin/app/job/query"
)

// nightlySpec is a normalized spec as the operator projects it, so the decode
// from the revision row can be asserted field by field.
const nightlySpec = `{"schedule":"*/5 * * * *","concurrencyPolicy":"Forbid","misfirePolicy":"Skip",` +
	`"timeoutSeconds":300,"jobTemplate":{"image":"curl:8","command":["curl"],"args":["-fsSL"],"backoffLimit":2},` +
	`"retryPolicy":{"maxAttempts":3}}`

func TestJobRepository_Get(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	created := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM job_definition_revisions WHERE tenant_id = \$1 AND id = \$2 AND is_active`).
		WithArgs("tenant-a", int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "source_mode", "source_uid", "source_namespace", "source_name",
			"generation", "spec_hash", "normalized_spec", "actor", "created_at",
		}).AddRow(
			11, "tenant-a", "kubernetes", "nightly", "jobs", "nightly",
			3, "hash-3", []byte(nightlySpec), "operator", created,
		))
	mock.ExpectCommit()

	item, err := NewJobRepository(db).Get(context.Background(), jobquery.GetInput{ID: 11, TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.ID != 11 || item.TenantID != "tenant-a" || item.SourceUID != "nightly" {
		t.Fatalf("identity did not survive the scan: %+v", item)
	}
	if item.Generation != 3 || item.SpecHash != "hash-3" || item.Actor != "operator" {
		t.Fatalf("revision fields did not survive the scan: %+v", item)
	}
	if item.Schedule != "*/5 * * * *" || item.Suspend {
		t.Errorf("schedule = %q suspend = %v", item.Schedule, item.Suspend)
	}
	if item.ConcurrencyPolicy != "Forbid" || item.MisfirePolicy != "Skip" {
		t.Errorf("policies = %s/%s", item.ConcurrencyPolicy, item.MisfirePolicy)
	}
	if item.TimeoutSeconds != 300 || item.RetryMaxAttempts != 3 {
		t.Errorf("timeout = %d retry = %d", item.TimeoutSeconds, item.RetryMaxAttempts)
	}
	if item.JobTemplate.Image != "curl:8" || item.JobTemplate.BackoffLimit != 2 {
		t.Errorf("job template = %+v", item.JobTemplate)
	}
	if item.ScheduleSummary != "cron: */5 * * * *" {
		t.Errorf("schedule summary = %q", item.ScheduleSummary)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestJobRepository_Get_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM job_definition_revisions`).
		WithArgs("tenant-a", int64(99)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err = NewJobRepository(db).Get(context.Background(), jobquery.GetInput{ID: 99, TenantID: "tenant-a"})
	assertNotFound(t, err, "job")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestJobRepository_Get_CorruptSpecSurfaces(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	created := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	expectTenantTx(t, mock, "tenant-a")
	// A revision row whose normalized spec does not parse is a write-side
	// defect; serving an empty job would read as "exists, does nothing".
	mock.ExpectQuery(`SELECT (.+) FROM job_definition_revisions`).
		WithArgs("tenant-a", int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "source_mode", "source_uid", "source_namespace", "source_name",
			"generation", "spec_hash", "normalized_spec", "actor", "created_at",
		}).AddRow(
			11, "tenant-a", "kubernetes", "nightly", "jobs", "nightly",
			3, "hash-3", []byte(`{not-json`), "operator", created,
		))
	mock.ExpectRollback()

	if _, err := NewJobRepository(db).Get(context.Background(), jobquery.GetInput{ID: 11, TenantID: "tenant-a"}); err == nil {
		t.Fatal("expected the corrupt spec to surface")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestJobRepository_List(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	created := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM job_definition_revisions WHERE tenant_id = \$1 AND is_active`).
		WithArgs("tenant-a", 50, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "source_namespace", "source_name", "source_uid",
			"generation", "actor", "normalized_spec", "created_at",
		}).
			AddRow(12, "tenant-a", "jobs", "hourly", "hourly", 1, "operator", []byte(`{"schedule":"0 * * * *"}`), created).
			AddRow(11, "tenant-a", "jobs", "nightly", "nightly", 3, "operator", []byte(nightlySpec), created))
	mock.ExpectCommit()

	items, err := NewJobRepository(db).List(context.Background(), jobquery.ListInput{TenantID: "tenant-a", Limit: 50, Offset: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Name != "hourly" || items[0].ScheduleSummary != "cron: 0 * * * *" {
		t.Errorf("items[0] = %+v", items[0])
	}
	if items[1].Schedule != "*/5 * * * *" || items[1].ConcurrencyPolicy != "Forbid" {
		t.Errorf("items[1] = %+v", items[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestJobRepository_List_SuspendedSummary(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	created := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM job_definition_revisions`).
		WithArgs("tenant-a", 50, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "source_namespace", "source_name", "source_uid",
			"generation", "actor", "normalized_spec", "created_at",
		}).AddRow(11, "tenant-a", "jobs", "nightly", "nightly", 3, "operator",
			[]byte(`{"schedule":"*/5 * * * *","suspend":true}`), created))
	mock.ExpectCommit()

	// Suspend wins over the expression: a stopped definition must not be
	// described by the cron it no longer follows.
	items, err := NewJobRepository(db).List(context.Background(), jobquery.ListInput{TenantID: "tenant-a", Limit: 50, Offset: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 || items[0].ScheduleSummary != "suspended" {
		t.Fatalf("items = %+v, want a suspended summary", items)
	}
}

func TestJobRepository_List_Empty(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM job_definition_revisions`).
		WithArgs("tenant-a", 50, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "source_namespace", "source_name", "source_uid",
			"generation", "actor", "normalized_spec", "created_at",
		}))
	mock.ExpectCommit()

	items, err := NewJobRepository(db).List(context.Background(), jobquery.ListInput{TenantID: "tenant-a", Limit: 50, Offset: 0})
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
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM job_definition_revisions`).
		WithArgs("tenant-a", 50, 0).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := NewJobRepository(db).List(context.Background(), jobquery.ListInput{TenantID: "tenant-a", Limit: 50, Offset: 0}); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

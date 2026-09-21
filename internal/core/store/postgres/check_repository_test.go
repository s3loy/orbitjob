package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/domain/resource"
)

var checkColumns = []string{
	"id", "name", "description", "tenant_id", "resource_group_id", "status", "check_type",
	"check_config", "assertion_rules", "schedule_type", "cron_expr",
	"interval_sec", "timezone", "timeout_sec", "retry_limit",
	"priority", "labels", "next_run_at", "version", "created_at", "updated_at",
}

func newCheckRepoMock(t *testing.T) (*CheckRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewCheckRepository(db), mock
}

func expectSetConfig(mock sqlmock.Sqlmock, tenantID string) {
	mock.ExpectExec("SELECT set_config").
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectAuditInsert(mock sqlmock.Sqlmock) {
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestCheckRepository_Create(t *testing.T) {
	repo, mock := newCheckRepoMock(t)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	cron := "*/5 * * * *"

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("INSERT INTO checks").
		WithArgs(
			"test-check", sqlmock.AnyArg(), "default", "active", "http_health",
			sqlmock.AnyArg(), sqlmock.AnyArg(), "cron", cron, sqlmock.AnyArg(),
			"UTC", 30, 2, 5, sqlmock.AnyArg(), sqlmock.AnyArg(), nil,
		).
		WillReturnRows(sqlmock.NewRows(checkColumns).AddRow(
			1, "test-check", nil, "default", nil, "active", "http_health",
			[]byte("{}"), []byte("[]"), "cron", cron, nil,
			"UTC", 30, 2, 5, []byte("{}"), nil, 1, now, now,
		))
	expectAuditInsert(mock)
	mock.ExpectCommit()

	snap, err := repo.Create(context.Background(), check.CreateSpec{
		Name:         "test-check",
		TenantID:     "default",
		CheckType:    "http_health",
		ScheduleType: "cron",
		CronExpr:     &cron,
		Timezone:     "UTC",
		TimeoutSec:   30,
		RetryLimit:   2,
		Priority:     5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ID != 1 {
		t.Errorf("id = %d, want 1", snap.ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCheckRepository_Create_BeginTxError(t *testing.T) {
	repo, mock := newCheckRepoMock(t)
	mock.ExpectBegin().WillReturnError(errors.New("boom"))

	_, err := repo.Create(context.Background(), check.CreateSpec{
		Name:         "test",
		TenantID:     "default",
		CheckType:    "http_health",
		ScheduleType: "cron",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckRepository_ChangeStatus_Pause(t *testing.T) {
	repo, mock := newCheckRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("SELECT status FROM checks").
		WithArgs("default", int64(1), nil).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	mock.ExpectQuery("UPDATE checks").
		WithArgs("paused", "default", int64(1), 1).
		WillReturnRows(sqlmock.NewRows(checkColumns).AddRow(
			1, "test", nil, "default", nil, "paused", "http_health",
			nil, nil, "cron", nil, nil, "UTC", 30, 2, 5, nil, nil, 2,
			time.Now(), time.Now(),
		))
	expectAuditInsert(mock)
	mock.ExpectCommit()

	snap, err := repo.ChangeStatus(context.Background(), "default", "", 1, 1, "pause")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Status != "paused" {
		t.Errorf("status = %q, want paused", snap.Status)
	}
}

func TestCheckRepository_ChangeStatus_NotFound(t *testing.T) {
	repo, mock := newCheckRepoMock(t)

	expectSetConfig(mock, "default")
	mock.ExpectQuery("SELECT status FROM checks").
		WithArgs("default", int64(99), nil).
		WillReturnError(sql.ErrNoRows)

	_, err := repo.ChangeStatus(context.Background(), "default", "", 99, 1, "pause")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckRepository_ChangeStatus_VersionConflict(t *testing.T) {
	repo, mock := newCheckRepoMock(t)

	expectSetConfig(mock, "default")
	mock.ExpectQuery("SELECT status FROM checks").
		WithArgs("default", int64(1), nil).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	mock.ExpectQuery("UPDATE checks").
		WithArgs("paused", "default", int64(1), 1).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id FROM checks").
		WithArgs("default", int64(1), nil).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectRollback()

	_, err := repo.ChangeStatus(context.Background(), "default", "", 1, 1, "pause")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckRepository_Delete(t *testing.T) {
	repo, mock := newCheckRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("UPDATE checks SET deleted_at").
		WithArgs("default", int64(1), 1, nil).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	expectAuditInsert(mock)
	mock.ExpectCommit()

	if err := repo.Delete(context.Background(), "default", "", 1, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckRepository_Delete_NotFound(t *testing.T) {
	repo, mock := newCheckRepoMock(t)

	expectSetConfig(mock, "default")
	mock.ExpectQuery("UPDATE checks SET deleted_at").
		WithArgs("default", int64(99), 1, nil).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id FROM checks").
		WithArgs("default", int64(99), nil).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	err := repo.Delete(context.Background(), "default", "", 99, 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckRepository_ListDue(t *testing.T) {
	repo, mock := newCheckRepoMock(t)
	now := time.Now()

	// The scan runs inside a tenant-scoped transaction: RLS hides every row
	// unless app.tenant_id is set on the session.
	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("default").
		WillReturnResult(sqlmock.NewResult(0, 0))
	rows := sqlmock.NewRows(checkColumns)
	rows.AddRow(
		1, "health", nil, "default", "01JBGROUP0000000000000000RG", "active", "http_health",
		nil, nil, "cron", "*/5 * * * *", nil, "UTC", 30, 2, 5, nil, nil, 1, now, now,
	)
	mock.ExpectQuery("SELECT .+ FROM checks WHERE tenant_id").
		WithArgs("default", sqlmock.AnyArg(), 50).
		WillReturnRows(rows)
	mock.ExpectCommit()

	checks, err := repo.ListDue(context.Background(), "default", now, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Name != "health" {
		t.Errorf("name = %q, want health", checks[0].Name)
	}
	// The group rides with the snapshot: the check scheduler passes it to the
	// revision materializer, which stamps it on the check's ledger revision.
	if checks[0].ResourceGroupID != "01JBGROUP0000000000000000RG" {
		t.Errorf("resource_group_id = %q, want the check row's group", checks[0].ResourceGroupID)
	}
}

func TestCheckRepository_ListDue_Empty(t *testing.T) {
	repo, mock := newCheckRepoMock(t)
	now := time.Now()

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("default").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT .+ FROM checks WHERE tenant_id").
		WithArgs("default", sqlmock.AnyArg(), 50).
		WillReturnRows(sqlmock.NewRows(checkColumns))
	mock.ExpectCommit()

	checks, err := repo.ListDue(context.Background(), "default", now, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(checks) != 0 {
		t.Errorf("expected 0 checks, got %d", len(checks))
	}
}

func TestCheckRepository_UpdateNextRunAt(t *testing.T) {
	repo, mock := newCheckRepoMock(t)
	next := time.Now().Add(time.Minute)

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("default").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE checks SET next_run_at").
		WithArgs(next, "default", int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.UpdateNextRunAt(context.Background(), "default", 1, next); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckRepository_UpdateNextRunAt_MissingCheck(t *testing.T) {
	repo, mock := newCheckRepoMock(t)
	next := time.Now().Add(time.Minute)

	// An update matching no rows must surface as not-found: a cursor write
	// that silently did nothing would make the check fire again next tick.
	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("default").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE checks SET next_run_at").
		WithArgs(next, "default", int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	if err := repo.UpdateNextRunAt(context.Background(), "default", 1, next); err == nil {
		t.Fatal("expected not-found for a check the update did not touch")
	}
}

func TestCheckRepository_GetByID(t *testing.T) {
	repo, mock := newCheckRepoMock(t)
	now := time.Now()

	// GetByID reads inside a tenant-scoped transaction so the checks table's
	// RLS policy binds; the set_config expectation pins that.
	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("default").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT .+ FROM checks WHERE tenant_id").
		WithArgs("default", int64(1)).
		WillReturnRows(sqlmock.NewRows(checkColumns).AddRow(
			1, "health", nil, "default", nil, "active", "http_health",
			nil, nil, "cron", nil, nil, "UTC", 30, 2, 5, nil, nil, 1, now, now,
		))
	mock.ExpectCommit()

	snap, err := repo.GetByID(context.Background(), "default", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ID != 1 {
		t.Errorf("id = %d, want 1", snap.ID)
	}
}

func TestCheckRepository_GetByID_NotFound(t *testing.T) {
	repo, mock := newCheckRepoMock(t)

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("default").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT .+ FROM checks WHERE tenant_id").
		WithArgs("default", int64(99)).
		WillReturnError(sql.ErrNoRows)
	// The not-found path returns the scanErr rather than the outer err the old
	// conditional rollback was keyed on; the rollback is now unconditional, so
	// it fires here too. Pinned in detail by
	// TestCheckRepository_GetByID_NotFound_RollsBack.
	mock.ExpectRollback()

	_, err := repo.GetByID(context.Background(), "default", 99)
	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *resource.NotFoundError, got %T: %v", err, err)
	}
	if notFound.Resource != "check" {
		t.Errorf("resource = %q, want check", notFound.Resource)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

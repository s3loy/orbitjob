package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/resource"
)

// sloColumns is the projection every slos query scans, in source order.
var sloColumns = []string{
	"id", "tenant_id", "name", "description", "sli_id", "target",
	"window_type", "window_duration", "alert_fast_burn_rate",
	"alert_slow_burn_rate", "status", "version", "created_at", "updated_at",
}

func newSLORepoMock(t *testing.T) (*SLORepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewSLORepository(db), mock
}

// Every SLORepository method now opens its transaction and binds the tenant
// GUC before touching slos -- the slos RLS policy fails closed without it.
// The expectations below each carry the Begin/set_config/Commit-or-Rollback
// lifecycle for that reason; a query issued on the bare pool would leave the
// write path denied by WITH CHECK and the read path silently empty under the
// deployment roles.

func TestSLORepository_Create(t *testing.T) {
	repo, mock := newSLORepoMock(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	spec := slo.CreateSpec{
		Name:              "checkout-slo",
		SLIID:             7,
		Target:            0.99,
		WindowType:        "rolling",
		WindowDuration:    time.Hour,
		AlertFastBurnRate: 14.4,
		AlertSlowBurnRate: 6,
	}

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	// window_duration travels as seconds and comes back as a duration.
	mock.ExpectQuery("INSERT INTO slos").
		WithArgs(
			"default", "checkout-slo", nil, int64(7), 0.99, "rolling",
			time.Hour/time.Second, 14.4, float64(6), nil,
		).
		WillReturnRows(sqlmock.NewRows(sloColumns).AddRow(
			3, "default", "checkout-slo", nil, int64(7), 0.99,
			"rolling", int64(86400), 14.4, 6, "active", 1, now, now,
		))
	mock.ExpectCommit()

	snap, err := repo.Create(context.Background(), "default", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ID != 3 {
		t.Errorf("id = %d, want 3", snap.ID)
	}
	if snap.TenantID != "default" {
		t.Errorf("tenant_id = %q, want default", snap.TenantID)
	}
	if snap.SLIID != 7 {
		t.Errorf("sli_id = %d, want 7", snap.SLIID)
	}
	if snap.WindowDuration != 24*time.Hour {
		t.Errorf("window_duration = %v, want 24h", snap.WindowDuration)
	}
	if snap.Status != "active" {
		t.Errorf("status = %q, want active", snap.Status)
	}
	if snap.Version != 1 {
		t.Errorf("version = %d, want 1", snap.Version)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLORepository_Create_Error(t *testing.T) {
	repo, mock := newSLORepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("INSERT INTO slos").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	if _, err := repo.Create(context.Background(), "default", slo.CreateSpec{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestSLORepository_ChangeStatus(t *testing.T) {
	repo, mock := newSLORepoMock(t)
	now := time.Now()

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("UPDATE slos SET status = \\$1").
		WithArgs("paused", "default", int64(3), 1, nil).
		WillReturnRows(sqlmock.NewRows(sloColumns).AddRow(
			3, "default", "checkout-slo", nil, int64(7), 0.99,
			"rolling", int64(3600), 14.4, 6, "paused", 2, now, now,
		))
	mock.ExpectCommit()

	snap, err := repo.ChangeStatus(context.Background(), "default", "", 3, 1, "paused")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Status != "paused" {
		t.Errorf("status = %q, want paused", snap.Status)
	}
	if snap.Version != 2 {
		t.Errorf("version = %d, want 2", snap.Version)
	}
	if snap.WindowDuration != time.Hour {
		t.Errorf("window_duration = %v, want 1h", snap.WindowDuration)
	}
}

func TestSLORepository_ChangeStatus_NotFound(t *testing.T) {
	repo, mock := newSLORepoMock(t)

	// The update misses, so the repository reads the row back -- inside the
	// same tenant transaction -- to tell a version conflict apart from a row
	// that is not there at all.
	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("UPDATE slos SET status = \\$1").
		WithArgs("paused", "default", int64(99), 1, nil).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id FROM slos").
		WithArgs("default", int64(99), nil).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err := repo.ChangeStatus(context.Background(), "default", "", 99, 1, "paused")
	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *resource.NotFoundError, got %T: %v", err, err)
	}
	if notFound.Resource != "slo" {
		t.Errorf("resource = %q, want slo", notFound.Resource)
	}
}

func TestSLORepository_ChangeStatus_VersionConflict(t *testing.T) {
	repo, mock := newSLORepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("UPDATE slos SET status = \\$1").
		WithArgs("paused", "default", int64(3), 1, nil).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id FROM slos").
		WithArgs("default", int64(3), nil).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
	mock.ExpectRollback()

	_, err := repo.ChangeStatus(context.Background(), "default", "", 3, 1, "paused")
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected *resource.ConflictError, got %T: %v", err, err)
	}
	if conflict.Field != "version" {
		t.Errorf("field = %q, want version", conflict.Field)
	}
}

func TestSLORepository_Delete(t *testing.T) {
	repo, mock := newSLORepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectExec("UPDATE slos SET deleted_at").
		WithArgs("default", int64(3), 1, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.Delete(context.Background(), "default", "", 3, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSLORepository_Delete_NotFound(t *testing.T) {
	repo, mock := newSLORepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectExec("UPDATE slos SET deleted_at").
		WithArgs("default", int64(99), 1, nil).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT id FROM slos").
		WithArgs("default", int64(99), nil).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	err := repo.Delete(context.Background(), "default", "", 99, 1)
	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *resource.NotFoundError, got %T: %v", err, err)
	}
	if notFound.Resource != "slo" {
		t.Errorf("resource = %q, want slo", notFound.Resource)
	}
}

func TestSLORepository_Delete_VersionConflict(t *testing.T) {
	repo, mock := newSLORepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectExec("UPDATE slos SET deleted_at").
		WithArgs("default", int64(3), 1, nil).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT id FROM slos").
		WithArgs("default", int64(3), nil).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
	mock.ExpectRollback()

	err := repo.Delete(context.Background(), "default", "", 3, 1)
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected *resource.ConflictError, got %T: %v", err, err)
	}
	if conflict.Field != "version" {
		t.Errorf("field = %q, want version", conflict.Field)
	}
}

func TestSLORepository_Delete_DBError(t *testing.T) {
	repo, mock := newSLORepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectExec("UPDATE slos SET deleted_at").
		WithArgs("default", int64(3), 1, nil).
		WillReturnError(errors.New("delete failed"))
	mock.ExpectRollback()

	if err := repo.Delete(context.Background(), "default", "", 3, 1); err == nil {
		t.Fatal("expected error")
	}
}

func TestSLORepository_ListActive(t *testing.T) {
	repo, mock := newSLORepoMock(t)
	now := time.Now()

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("SELECT (.+) FROM slos WHERE tenant_id = \\$1 AND status = 'active'").
		WithArgs("default").
		WillReturnRows(sqlmock.NewRows(sloColumns).
			AddRow(3, "default", "checkout-slo", nil, int64(7), 0.99,
				"rolling", int64(3600), 14.4, 6, "active", 1, now, now).
			AddRow(4, "default", "signup-slo", nil, int64(8), 0.999,
				"calendar", int64(604800), 14.4, 6, "active", 1, now, now))
	mock.ExpectCommit()

	slos, err := repo.ListActive(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(slos) != 2 {
		t.Fatalf("expected 2 slos, got %d", len(slos))
	}
	if slos[0].WindowDuration != time.Hour {
		t.Errorf("slos[0].window_duration = %v, want 1h", slos[0].WindowDuration)
	}
	if slos[1].WindowDuration != 7*24*time.Hour {
		t.Errorf("slos[1].window_duration = %v, want 168h", slos[1].WindowDuration)
	}
	if slos[1].Name != "signup-slo" {
		t.Errorf("slos[1].name = %q, want signup-slo", slos[1].Name)
	}
}

func TestSLORepository_ListActive_Empty(t *testing.T) {
	repo, mock := newSLORepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("SELECT (.+) FROM slos WHERE tenant_id = \\$1 AND status = 'active'").
		WithArgs("default").
		WillReturnRows(sqlmock.NewRows(sloColumns))
	mock.ExpectCommit()

	slos, err := repo.ListActive(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(slos) != 0 {
		t.Errorf("expected 0 slos, got %d", len(slos))
	}
}

func TestSLORepository_ListActive_DBError(t *testing.T) {
	repo, mock := newSLORepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("SELECT (.+) FROM slos").
		WithArgs("default").
		WillReturnError(errors.New("query failed"))
	mock.ExpectRollback()

	if _, err := repo.ListActive(context.Background(), "default"); err == nil {
		t.Fatal("expected error")
	}
}

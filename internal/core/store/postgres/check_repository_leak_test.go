package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// The check repository used to release its transactions only through
// `defer func() { if err != nil { tx.Rollback() } }()` keyed on the outer err
// variable, while several returns leave err nil: GetByID's row scan captured
// its error into scanErr, ChangeStatus's unknown-action return fired before
// err was ever assigned, and ListDue's scan and rows.Err checks capture their
// errors into shadowed locals. Every one of those returns leaked the
// transaction -- holding the app.tenant_id GUC -- and pinned a pooled
// connection per call. These tests pin the rollback on exactly those paths:
// an unmet Rollback expectation is the leak.

func TestCheckRepository_GetByID_NotFound_RollsBack(t *testing.T) {
	repo, mock := newCheckRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("SELECT .+ FROM checks WHERE tenant_id").
		WithArgs("default", int64(99)).
		WillReturnError(sql.ErrNoRows)
	// The not-found return carries scanErr, not the outer err the old
	// conditional release was keyed on, so this rollback only happens once the
	// release no longer depends on that variable.
	mock.ExpectRollback()

	if _, err := repo.GetByID(context.Background(), "default", 99); err == nil {
		t.Fatal("expected not-found error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("transaction lifecycle incomplete on the not-found path: %v", err)
	}
}

func TestCheckRepository_GetByID_ScanFailure_RollsBack(t *testing.T) {
	repo, mock := newCheckRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	// A scan failure other than ErrNoRows is captured into scanErr the same
	// way and must not strand the transaction either.
	mock.ExpectQuery("SELECT .+ FROM checks WHERE tenant_id").
		WithArgs("default", int64(1)).
		WillReturnError(errors.New("read: connection reset"))
	mock.ExpectRollback()

	if _, err := repo.GetByID(context.Background(), "default", 1); err == nil {
		t.Fatal("expected scan error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("transaction lifecycle incomplete on the scan-failure path: %v", err)
	}
}

func TestCheckRepository_ChangeStatus_UnknownAction_RollsBack(t *testing.T) {
	repo, mock := newCheckRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("SELECT status FROM checks").
		WithArgs("default", int64(7), nil).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	// The unknown-action return happens before err is ever assigned, so the
	// err-keyed release never fired on it.
	mock.ExpectRollback()

	_, err := repo.ChangeStatus(context.Background(), "default", "", 7, 1, "restart")
	if err == nil || !strings.Contains(err.Error(), "unknown action: restart") {
		t.Fatalf("expected unknown-action error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("transaction lifecycle incomplete on the unknown-action path: %v", err)
	}
}

func TestCheckRepository_ListDue_ScanError_RollsBack(t *testing.T) {
	repo, mock := newCheckRepoMock(t)
	now := time.Now()

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	// timeout_sec is a string, so the row Scan itself fails; the loop captures
	// that error into a local, which the err-keyed release never saw.
	mock.ExpectQuery("SELECT .+ FROM checks WHERE tenant_id").
		WithArgs("default", sqlmock.AnyArg(), 50).
		WillReturnRows(sqlmock.NewRows(checkColumns).AddRow(
			1, "health", nil, "default", nil, "active", "http_health",
			nil, nil, "cron", nil, nil, "UTC", "boom", 2, 5, nil, nil, 1, now, now,
		))
	mock.ExpectRollback()

	_, err := repo.ListDue(context.Background(), "default", now, 50)
	if err == nil || !strings.Contains(err.Error(), "scan check") {
		t.Fatalf("expected scan error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("transaction lifecycle incomplete on the scan-error path: %v", err)
	}
}

func TestCheckRepository_ListDue_IterationError_RollsBack(t *testing.T) {
	repo, mock := newCheckRepoMock(t)
	now := time.Now()

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	// RowError makes the driver's Next fail on the second row, so one row
	// scans fine and the loop then exits through rows.Err() -- also captured
	// into a local the err-keyed release never saw.
	mock.ExpectQuery("SELECT .+ FROM checks WHERE tenant_id").
		WithArgs("default", sqlmock.AnyArg(), 50).
		WillReturnRows(sqlmock.NewRows(checkColumns).
			AddRow(1, "health", nil, "default", nil, "active", "http_health",
				nil, nil, "cron", nil, nil, "UTC", 30, 2, 5, nil, nil, 1, now, now).
			AddRow(2, "backup", nil, "default", nil, "active", "http_health",
				nil, nil, "cron", nil, nil, "UTC", 30, 2, 5, nil, nil, 1, now, now).
			RowError(1, errors.New("driver: connection reset mid-iteration")))
	mock.ExpectRollback()

	_, err := repo.ListDue(context.Background(), "default", now, 50)
	if err == nil || !strings.Contains(err.Error(), "iterate checks") {
		t.Fatalf("expected iteration error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("transaction lifecycle incomplete on the iteration-error path: %v", err)
	}
}

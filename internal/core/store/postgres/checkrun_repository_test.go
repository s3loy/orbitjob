package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"orbitjob/internal/core/domain/checkrun"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func newCheckRunRepoMock(t *testing.T) (*CheckRunRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewCheckRunRepository(db), mock
}

func completedRecord() checkrun.CompletedRecord {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	return checkrun.CompletedRecord{
		RunID:       "0f1e2d3c-4b5a-6c7d-8e9f-0a1b2c3d4e5f",
		CheckID:     7,
		Status:      checkrun.StatusSuccess,
		ScheduledAt: now.Add(-time.Minute),
		StartedAt:   now.Add(-30 * time.Second),
		FinishedAt:  now,
		DurationMs:  30000,
	}
}

// TestCheckRunRepository_RecordsCompleted pins the read model's write: one
// deterministic run id upserts one terminal row, guarded by ON CONFLICT so a
// replayed recording cannot double the history.
func TestCheckRunRepository_RecordsCompleted(t *testing.T) {
	repo, mock := newCheckRunRepoMock(t)
	rec := completedRecord()

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("default").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("INSERT INTO check_runs").
		WithArgs(rec.RunID, "default", rec.CheckID, rec.Status,
			rec.ScheduledAt, rec.StartedAt, rec.FinishedAt, rec.DurationMs).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(99))
	mock.ExpectCommit()

	if err := repo.RecordCompleted(context.Background(), "default", rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestCheckRunRepository_RecordsFailure covers the other terminal status the
// exit-code contract produces.
func TestCheckRunRepository_RecordsFailure(t *testing.T) {
	repo, mock := newCheckRunRepoMock(t)
	rec := completedRecord()
	rec.Status = checkrun.StatusFailed

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("default").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("INSERT INTO check_runs").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(100))
	mock.ExpectCommit()

	if err := repo.RecordCompleted(context.Background(), "default", rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCheckRunRepository_RejectsNonTerminalStatus(t *testing.T) {
	repo, mock := newCheckRunRepoMock(t)

	// A status the schema's CHECK would refuse must be refused here, where a
	// caller can read it, rather than aborting a transaction.
	for _, status := range []string{checkrun.StatusPending, checkrun.StatusRunning, "canceled"} {
		rec := completedRecord()
		rec.Status = status
		if err := repo.RecordCompleted(context.Background(), "default", rec); err == nil {
			t.Fatalf("status %q accepted", status)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("refused input reached the database: %v", err)
	}
}

func TestCheckRunRepository_RejectsBlankRunID(t *testing.T) {
	repo, _ := newCheckRunRepoMock(t)
	rec := completedRecord()
	rec.RunID = ""
	if err := repo.RecordCompleted(context.Background(), "default", rec); err == nil {
		t.Fatal("expected a blank run id to be refused")
	}
}

func TestCheckRunRepository_DBError(t *testing.T) {
	repo, mock := newCheckRunRepoMock(t)
	rec := completedRecord()

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("default").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("INSERT INTO check_runs").
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	err := repo.RecordCompleted(context.Background(), "default", rec)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "db down") {
		t.Fatalf("error lost the cause: %v", err)
	}
}

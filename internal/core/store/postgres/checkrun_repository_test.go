package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

var checkRunColumns = []string{
	"id", "run_id", "tenant_id", "check_id", "status", "severity",
	"output", "evaluation_result", "scheduled_at", "started_at",
	"finished_at", "duration_ms", "version", "created_at",
}

func newCheckRunRepoMock(t *testing.T) (*CheckRunRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewCheckRunRepository(db), mock
}

func TestCheckRunRepository_Create(t *testing.T) {
	repo, mock := newCheckRunRepoMock(t)
	now := time.Now()

	mock.ExpectQuery("INSERT INTO check_runs").
		WithArgs("default", int64(1), "pending", now).
		WillReturnRows(sqlmock.NewRows(checkRunColumns).AddRow(
			1, "run-abc", "default", 1, "pending", nil,
			nil, nil, now, nil, nil, nil, 1, now,
		))

	snap, err := repo.Create(context.Background(), "default", 1, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ID != 1 {
		t.Errorf("id = %d, want 1", snap.ID)
	}
	if snap.RunID != "run-abc" {
		t.Errorf("run_id = %q, want run-abc", snap.RunID)
	}
}

func TestCheckRunRepository_Create_Error(t *testing.T) {
	repo, mock := newCheckRunRepoMock(t)
	now := time.Now()

	mock.ExpectQuery("INSERT INTO check_runs").
		WithArgs("default", int64(1), "pending", now).
		WillReturnError(errors.New("insert failed"))

	_, err := repo.Create(context.Background(), "default", 1, now)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckRunRepository_ClaimNext(t *testing.T) {
	repo, mock := newCheckRunRepoMock(t)
	now := time.Now()

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("default").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("UPDATE check_runs").
		WithArgs(now, "default", 10).
		WillReturnRows(sqlmock.NewRows(checkRunColumns).AddRow(
			1, "run-1", "default", 1, "running", nil,
			nil, nil, now, now, nil, nil, 1, now,
		))
	mock.ExpectCommit()

	runs, err := repo.ClaimNext(context.Background(), "default", 10, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Status != "running" {
		t.Errorf("status = %q, want running", runs[0].Status)
	}
}

func TestCheckRunRepository_ClaimNext_Empty(t *testing.T) {
	repo, mock := newCheckRunRepoMock(t)
	now := time.Now()

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("default").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("UPDATE check_runs").
		WithArgs(now, "default", 10).
		WillReturnRows(sqlmock.NewRows(checkRunColumns))
	mock.ExpectCommit()

	runs, err := repo.ClaimNext(context.Background(), "default", 10, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("expected 0 runs, got %d", len(runs))
	}
}

func TestCheckRunRepository_ClaimNext_BeginTxError(t *testing.T) {
	repo, mock := newCheckRunRepoMock(t)
	now := time.Now()

	mock.ExpectBegin().WillReturnError(errors.New("boom"))

	_, err := repo.ClaimNext(context.Background(), "default", 10, now)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckRunRepository_Complete(t *testing.T) {
	repo, mock := newCheckRunRepoMock(t)
	now := time.Now()
	output := map[string]any{"status_code": 200}
	evalResult := map[string]any{"overall_severity": "ok"}

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("default").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("UPDATE check_runs").
		WithArgs("success", "ok", sqlmock.AnyArg(), sqlmock.AnyArg(), 100, now, "default", int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	if err := repo.Complete(context.Background(), "default", 1, "success", "ok", output, evalResult, 100, now); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckRunRepository_Complete_NotRunning(t *testing.T) {
	repo, mock := newCheckRunRepoMock(t)
	now := time.Now()

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config").
		WithArgs("default").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("UPDATE check_runs").
		WithArgs("success", "ok", sqlmock.AnyArg(), sqlmock.AnyArg(), 0, now, "default", int64(99)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	err := repo.Complete(context.Background(), "default", 99, "success", "ok", nil, nil, 0, now)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckRunRepository_Complete_BeginTxError(t *testing.T) {
	repo, mock := newCheckRunRepoMock(t)
	now := time.Now()

	mock.ExpectBegin().WillReturnError(errors.New("boom"))

	err := repo.Complete(context.Background(), "default", 1, "success", "ok", nil, nil, 0, now)
	if err == nil {
		t.Fatal("expected error")
	}
}

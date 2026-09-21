package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func newSLISnapshotRepoMock(t *testing.T) (*SLISnapshotRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewSLISnapshotRepository(db), mock
}

func TestSLISnapshotRepository_IncrementSnapshot_Good(t *testing.T) {
	repo, mock := newSLISnapshotRepoMock(t)
	windowStart := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	// The window end is derived from the start, and a good event contributes a
	// delta of 1.
	mock.ExpectExec("INSERT INTO sli_snapshots").
		WithArgs("default", int64(7), windowStart, windowStart.Add(5*time.Minute), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.IncrementSnapshot(context.Background(), "default", 7, windowStart, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLISnapshotRepository_IncrementSnapshot_Bad(t *testing.T) {
	repo, mock := newSLISnapshotRepoMock(t)
	windowStart := time.Date(2026, 6, 1, 12, 5, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectExec("INSERT INTO sli_snapshots").
		WithArgs("default", int64(7), windowStart, windowStart.Add(5*time.Minute), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.IncrementSnapshot(context.Background(), "default", 7, windowStart, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLISnapshotRepository_IncrementSnapshot_Error(t *testing.T) {
	repo, mock := newSLISnapshotRepoMock(t)
	windowStart := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectExec("INSERT INTO sli_snapshots").
		WithArgs("default", int64(7), windowStart, windowStart.Add(5*time.Minute), int64(1)).
		WillReturnError(errors.New("upsert failed"))
	mock.ExpectRollback()

	if err := repo.IncrementSnapshot(context.Background(), "default", 7, windowStart, true); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLISnapshotRepository_AggregateWindow(t *testing.T) {
	repo, mock := newSLISnapshotRepoMock(t)
	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(good_events_count\\), 0\\), COALESCE\\(SUM\\(total_events_count\\), 0\\)").
		WithArgs("default", int64(7), start, end).
		WillReturnRows(sqlmock.NewRows([]string{"good", "total"}).AddRow(int64(3), int64(4)))
	mock.ExpectCommit()

	agg, err := repo.AggregateWindow(context.Background(), "default", 7, start, end)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agg.GoodEventsCount != 3 || agg.TotalEventsCount != 4 {
		t.Fatalf("counts = (%d, %d), want (3, 4)", agg.GoodEventsCount, agg.TotalEventsCount)
	}
	if agg.SLIValue != 0.75 {
		t.Errorf("sli_value = %v, want 0.75", agg.SLIValue)
	}
}

func TestSLISnapshotRepository_AggregateWindow_NoEvents(t *testing.T) {
	repo, mock := newSLISnapshotRepoMock(t)
	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(good_events_count\\), 0\\), COALESCE\\(SUM\\(total_events_count\\), 0\\)").
		WithArgs("default", int64(7), start, end).
		WillReturnRows(sqlmock.NewRows([]string{"good", "total"}).AddRow(int64(0), int64(0)))
	mock.ExpectCommit()

	agg, err := repo.AggregateWindow(context.Background(), "default", 7, start, end)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A window with no events has no SLI value; dividing would be meaningless.
	if agg.SLIValue != 0 {
		t.Errorf("sli_value = %v, want 0", agg.SLIValue)
	}
}

func TestSLISnapshotRepository_AggregateWindow_Error(t *testing.T) {
	repo, mock := newSLISnapshotRepoMock(t)
	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(good_events_count\\), 0\\)").
		WithArgs("default", int64(7), start, end).
		WillReturnError(errors.New("aggregate failed"))
	mock.ExpectRollback()

	if _, err := repo.AggregateWindow(context.Background(), "default", 7, start, end); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

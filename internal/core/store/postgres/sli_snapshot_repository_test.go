package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/platform/scan"
)

var sliAggregateColumns = []string{"good", "total", "last_event_at", "last_sample_at", "statistics_semantics", "source_available", "missing_reason", "source_uid"}

const sliAggregateQuery = `SELECT COALESCE\(SUM\(good_events_count\), 0\), COALESCE\(SUM\(total_events_count\), 0\).* FROM slis s LEFT JOIN sli_snapshots p ON p.tenant_id=s.tenant_id AND p.sli_id=s.id AND p.window_start >= \$3 AND p.window_start < \$4.* WHERE s.tenant_id = \$1 AND s.id = \$2`

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
	expectSetConfig(mock, coreFixtureSliSnapshotRepositoryTenantID)
	// The window end is derived from the start, and a good event contributes a
	// delta of 1.
	mock.ExpectExec("INSERT INTO sli_snapshots").
		WithArgs(coreFixtureSliSnapshotRepositoryTenantID, int64(7), windowStart, windowStart.Add(5*time.Minute), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.IncrementSnapshot(context.Background(), coreFixtureSliSnapshotRepositoryTenantID, 7, windowStart, true); err != nil {
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
	expectSetConfig(mock, coreFixtureSliSnapshotRepositoryTenantID)
	mock.ExpectExec("INSERT INTO sli_snapshots").
		WithArgs(coreFixtureSliSnapshotRepositoryTenantID, int64(7), windowStart, windowStart.Add(5*time.Minute), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.IncrementSnapshot(context.Background(), coreFixtureSliSnapshotRepositoryTenantID, 7, windowStart, false); err != nil {
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
	expectSetConfig(mock, coreFixtureSliSnapshotRepositoryTenantID)
	mock.ExpectExec("INSERT INTO sli_snapshots").
		WithArgs(coreFixtureSliSnapshotRepositoryTenantID, int64(7), windowStart, windowStart.Add(5*time.Minute), int64(1)).
		WillReturnError(errors.New("upsert failed"))
	mock.ExpectRollback()

	if err := repo.IncrementSnapshot(context.Background(), coreFixtureSliSnapshotRepositoryTenantID, 7, windowStart, true); err == nil {
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
	lastEventAt := end.Add(-time.Minute)
	lastSampleAt := lastEventAt.Add(-time.Second)

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureSliSnapshotRepositoryTenantID)
	mock.ExpectQuery(sliAggregateQuery).
		WithArgs(coreFixtureSliSnapshotRepositoryTenantID, int64(7), start, end).
		WillReturnRows(sqlmock.NewRows(sliAggregateColumns).AddRow(int64(3), int64(4), lastEventAt, lastSampleAt, "mixed", true, "", "scheduled-uid"))
	mock.ExpectCommit()

	agg, err := repo.AggregateWindow(context.Background(), coreFixtureSliSnapshotRepositoryTenantID, 7, start, end)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agg.GoodEventsCount != 3 || agg.TotalEventsCount != 4 {
		t.Fatalf("counts = (%d, %d), want (3, 4)", agg.GoodEventsCount, agg.TotalEventsCount)
	}
	if agg.SLIValue != 0.75 {
		t.Errorf("sli_value = %v, want 0.75", agg.SLIValue)
	}
	if agg.LastEventAt == nil || !agg.LastEventAt.Equal(lastEventAt) || agg.LastSampleAt == nil || !agg.LastSampleAt.Equal(lastSampleAt) ||
		agg.StatisticsSemantics != "mixed" || agg.SourceAvailable == nil || !*agg.SourceAvailable || agg.MissingReason != "" || agg.FreshUntil != nil {
		t.Fatalf("aggregate lost observation metadata or invented a sampling cadence: %+v", agg)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLISnapshotRepository_AggregateWindow_NoEvents(t *testing.T) {
	repo, mock := newSLISnapshotRepoMock(t)
	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureSliSnapshotRepositoryTenantID)
	mock.ExpectQuery(sliAggregateQuery).
		WithArgs(coreFixtureSliSnapshotRepositoryTenantID, int64(7), start, end).
		WillReturnRows(sqlmock.NewRows(sliAggregateColumns).AddRow(int64(0), int64(0), nil, nil, sli.EventTime, true, "task_timing_unavailable", "scheduled-uid"))
	mock.ExpectCommit()

	agg, err := repo.AggregateWindow(context.Background(), coreFixtureSliSnapshotRepositoryTenantID, 7, start, end)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A window with no events has no SLI value; dividing would be meaningless.
	if agg.SLIValue != 0 {
		t.Errorf("sli_value = %v, want 0", agg.SLIValue)
	}
	if agg.TotalEventsCount != 0 || agg.GoodEventsCount != 0 || agg.LastEventAt != nil || agg.LastSampleAt != nil ||
		agg.StatisticsSemantics != sli.EventTime || agg.SourceAvailable == nil || !*agg.SourceAvailable ||
		agg.MissingReason != "task_timing_unavailable" || agg.FreshUntil != nil {
		t.Fatalf("empty aggregate must retain missing evidence: %+v", agg)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLISnapshotRepository_AggregateWindow_CheckFreshness(t *testing.T) {
	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	lastSampleAt := end.Add(-time.Minute)
	for _, tc := range []struct {
		name   string
		status string
		active bool
	}{
		{name: "active interval source", status: "active", active: true},
		{name: "paused source", status: "paused", active: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newSLISnapshotRepoMock(t)
			mock.ExpectBegin()
			expectSetConfig(mock, coreFixtureSliSnapshotRepositoryTenantID)
			mock.ExpectQuery(sliAggregateQuery).
				WithArgs(coreFixtureSliSnapshotRepositoryTenantID, int64(7), start, end).
				WillReturnRows(sqlmock.NewRows(sliAggregateColumns).AddRow(3, 4, lastSampleAt, lastSampleAt, sli.EventTime, true, "", "check-9"))
			mock.ExpectQuery(`SELECT schedule_type,cron_expr,timezone,interval_sec,timeout_sec,retry_limit,status,deleted_at FROM checks WHERE tenant_id=\$1 AND id=\$2`).
				WithArgs(coreFixtureSliSnapshotRepositoryTenantID, int64(9)).
				WillReturnRows(sqlmock.NewRows([]string{"schedule_type", "cron_expr", "timezone", "interval_sec", "timeout_sec", "retry_limit", "status", "deleted_at"}).
					AddRow("interval", nil, "UTC", 60, 10, 1, tc.status, nil))
			mock.ExpectCommit()

			agg, err := repo.AggregateWindow(context.Background(), coreFixtureSliSnapshotRepositoryTenantID, 7, start, end)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if agg.SourceAvailable == nil || *agg.SourceAvailable != tc.active {
				t.Fatalf("source availability = %v, want %v", agg.SourceAvailable, tc.active)
			}
			if tc.active {
				// Two sampling intervals plus the configured execution grace.
				wantDeadline := lastSampleAt.Add(170 * time.Second)
				if agg.FreshUntil == nil || !agg.FreshUntil.Equal(wantDeadline) {
					t.Fatalf("fresh_until = %v, want %v", agg.FreshUntil, wantDeadline)
				}
			} else if agg.FreshUntil != nil {
				t.Fatalf("paused source must not publish a freshness deadline: %v", agg.FreshUntil)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unmet expectations: %v", err)
			}
		})
	}
}

func TestSLISnapshotRepository_AggregateWindow_Error(t *testing.T) {
	repo, mock := newSLISnapshotRepoMock(t)
	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureSliSnapshotRepositoryTenantID)
	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(good_events_count\\), 0\\)").
		WithArgs(coreFixtureSliSnapshotRepositoryTenantID, int64(7), start, end).
		WillReturnError(errors.New("aggregate failed"))
	mock.ExpectRollback()

	if _, err := repo.AggregateWindow(context.Background(), coreFixtureSliSnapshotRepositoryTenantID, 7, start, end); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

var coreFixtureSliSnapshotRepositoryTenantID = scan.GenerateID()

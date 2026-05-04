package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/app/schedule"
)

func TestSchedulerRepository_ScheduleOneDueCron_DecideRequired(t *testing.T) {
	repo := NewSchedulerRepository(nil)
	_, found, err := repo.ScheduleOneDueCron(context.Background(), time.Now().UTC(), nil)
	if err == nil {
		t.Fatalf("expected error when decide is nil")
	}
	if found {
		t.Fatalf("expected found=false")
	}
}

func TestSchedulerRepository_ScheduleOneDueCron_BeginTxError(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	mock.ExpectBegin().WillReturnError(errors.New("boom"))

	_, found, err := repo.ScheduleOneDueCron(context.Background(), time.Now().UTC(), schedule.DecideSchedule)
	if err == nil || !strings.Contains(err.Error(), "begin scheduler tx") {
		t.Fatalf("expected begin scheduler tx error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ScheduleOneDueCron_NoCandidateUnit(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectClaimNoRows(mock, now)
	mock.ExpectRollback()

	result, found, err := repo.ScheduleOneDueCron(context.Background(), now, schedule.DecideSchedule)
	if err != nil {
		t.Fatalf("ScheduleOneDueCron() error = %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	if result.Created {
		t.Fatalf("expected Created=false")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ScheduleOneDueCron_NoCandidateRollbackError(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectClaimNoRows(mock, now)
	mock.ExpectRollback().WillReturnError(errors.New("rb boom"))

	_, found, err := repo.ScheduleOneDueCron(context.Background(), now, schedule.DecideSchedule)
	if err == nil || !strings.Contains(err.Error(), "rollback empty scheduler tx") {
		t.Fatalf("expected rollback empty scheduler tx error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ScheduleOneDueCron_DecideError(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectClaimOneRow(mock, now, "tenant-a", 101, nil)
	expectSchedulerSetTenantContext(mock, "tenant-a")
	mock.ExpectRollback()

	_, found, err := repo.ScheduleOneDueCron(context.Background(), now, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{}, errors.New("decide boom")
	})
	if err == nil || !strings.Contains(err.Error(), "decide schedule policy") {
		t.Fatalf("expected decide schedule policy error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ScheduleOneDueCron_CreateWithoutScheduledAt(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := now.Add(5 * time.Minute)

	mock.ExpectBegin()
	expectClaimOneRow(mock, now, "tenant-a", 101, nil)
	expectSchedulerSetTenantContext(mock, "tenant-a")
	mock.ExpectRollback()

	_, found, err := repo.ScheduleOneDueCron(context.Background(), now, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{CreateInstance: true, NextRunAt: &next}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "scheduled_at is required") {
		t.Fatalf("expected scheduled_at required error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ScheduleOneDueCron_InsertError(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := now.Add(5 * time.Minute)
	scheduledAt := now
	partition := "part-1"

	mock.ExpectBegin()
	expectClaimOneRow(mock, now, "tenant-a", 101, &partition)
	expectSchedulerSetTenantContext(mock, "tenant-a")
	mock.ExpectQuery("INSERT INTO job_instances").
		WithArgs("tenant-a", int64(101), scheduledAt, 7, sqlmock.AnyArg(), 4).
		WillReturnError(errors.New("insert boom"))
	mock.ExpectRollback()

	_, found, err := repo.ScheduleOneDueCron(context.Background(), now, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{CreateInstance: true, ScheduledAt: &scheduledAt, NextRunAt: &next}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "insert scheduled instance") {
		t.Fatalf("expected insert scheduled instance error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ScheduleOneDueCron_NextRunAtRequired(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectClaimOneRow(mock, now, "tenant-a", 101, nil)
	expectSchedulerSetTenantContext(mock, "tenant-a")
	mock.ExpectRollback()

	_, found, err := repo.ScheduleOneDueCron(context.Background(), now, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{CreateInstance: false, NextRunAt: nil}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "next_run_at is required") {
		t.Fatalf("expected next_run_at required error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ScheduleOneDueCron_UpdateError(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := now.Add(5 * time.Minute)

	mock.ExpectBegin()
	expectClaimOneRow(mock, now, "tenant-a", 101, nil)
	expectSchedulerSetTenantContext(mock, "tenant-a")
	mock.ExpectExec("UPDATE jobs").
		WithArgs("tenant-a", int64(101), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(errors.New("update boom"))
	mock.ExpectRollback()

	_, found, err := repo.ScheduleOneDueCron(context.Background(), now, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{CreateInstance: false, NextRunAt: &next}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "update job schedule cursor") {
		t.Fatalf("expected update job schedule cursor error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ScheduleOneDueCron_CommitError(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := now.Add(5 * time.Minute)

	mock.ExpectBegin()
	expectClaimOneRow(mock, now, "tenant-a", 101, nil)
	expectSchedulerSetTenantContext(mock, "tenant-a")
	mock.ExpectExec("UPDATE jobs").
		WithArgs("tenant-a", int64(101), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit().WillReturnError(errors.New("commit boom"))

	_, found, err := repo.ScheduleOneDueCron(context.Background(), now, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{CreateInstance: false, NextRunAt: &next}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "commit scheduler tx") {
		t.Fatalf("expected commit scheduler tx error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ScheduleOneDueCron_SuccessWithoutInstance(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := now.Add(5 * time.Minute)

	mock.ExpectBegin()
	expectClaimOneRow(mock, now, "tenant-a", 101, nil)
	expectSchedulerSetTenantContext(mock, "tenant-a")
	mock.ExpectExec("UPDATE jobs").
		WithArgs("tenant-a", int64(101), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, found, err := repo.ScheduleOneDueCron(context.Background(), now, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{CreateInstance: false, NextRunAt: &next}, nil
	})
	if err != nil {
		t.Fatalf("ScheduleOneDueCron() error = %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if result.Created {
		t.Fatalf("expected Created=false")
	}
	if result.RunID != "" {
		t.Fatalf("expected RunID to be empty")
	}
	if result.NextRunAt == nil || !result.NextRunAt.Equal(next) {
		t.Fatalf("expected next_run_at in result")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ScheduleOneDueCron_SuccessWithInstance(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := now.Add(5 * time.Minute)
	scheduledAt := now.Add(-time.Minute)
	partition := "part-1"

	mock.ExpectBegin()
	expectClaimOneRow(mock, now, "tenant-a", 101, &partition)
	expectSchedulerSetTenantContext(mock, "tenant-a")
	mock.ExpectQuery("INSERT INTO job_instances").
		WithArgs("tenant-a", int64(101), scheduledAt, 7, sqlmock.AnyArg(), 4).
		WillReturnRows(sqlmock.NewRows([]string{"run_id"}).AddRow("run-1"))
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", "system", "scheduler", "instance.created", "instance", "run-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE jobs").
		WithArgs("tenant-a", int64(101), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, found, err := repo.ScheduleOneDueCron(context.Background(), now, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{CreateInstance: true, ScheduledAt: &scheduledAt, NextRunAt: &next}, nil
	})
	if err != nil {
		t.Fatalf("ScheduleOneDueCron() error = %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if !result.Created {
		t.Fatalf("expected Created=true")
	}
	if result.RunID != "run-1" {
		t.Fatalf("expected run_id=run-1, got %q", result.RunID)
	}
	if result.JobID != 101 || result.TenantID != "tenant-a" {
		t.Fatalf("unexpected identity fields in result: %+v", result)
	}
	assertMock(t, mock)
}

func newSchedulerRepoMock(t *testing.T) (*SchedulerRepository, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return NewSchedulerRepository(db), mock
}

func expectSchedulerSetTenantContext(mock sqlmock.Sqlmock, tenantID string) {
	mock.ExpectExec("SELECT set_config").
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectClaimNoRows(mock sqlmock.Sqlmock, now time.Time) {
	columns := []string{"id", "tenant_id", "priority", "partition_key", "retry_limit", "cron_expr", "timezone", "misfire_policy", "next_run_at"}
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(now).WillReturnRows(sqlmock.NewRows(columns))
}

func expectClaimOneRow(mock sqlmock.Sqlmock, now time.Time, tenantID string, jobID int64, partitionKey *string) {
	columns := []string{"id", "tenant_id", "priority", "partition_key", "retry_limit", "cron_expr", "timezone", "misfire_policy", "next_run_at"}
	rows := sqlmock.NewRows(columns)
	if partitionKey == nil {
		rows.AddRow(jobID, tenantID, 7, nil, 3, "*/5 * * * *", "UTC", "fire_now", now.Add(-time.Minute))
	} else {
		rows.AddRow(jobID, tenantID, 7, *partitionKey, 3, "*/5 * * * *", "UTC", "fire_now", now.Add(-time.Minute))
	}
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(now).WillReturnRows(rows)
}

func assertMock(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestClaimOneDueCronJob_ClaimError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(now).WillReturnError(errors.New("claim boom"))
	mock.ExpectRollback()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	_, found, err := claimOneDueCronJob(context.Background(), tx, now)
	if err == nil || !strings.Contains(err.Error(), "claim one due cron job") {
		t.Fatalf("expected wrapped claim error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	_ = tx.Rollback()

	assertMock(t, mock)
}

func TestUpdateJobScheduleCursor_NextRunAtRequired(t *testing.T) {
	err := updateJobScheduleCursor(context.Background(), nil, dueCronJobRecord{TenantID: "tenant-a", ID: 1}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "next_run_at is required") {
		t.Fatalf("expected next_run_at required error, got %v", err)
	}
}

func TestNewSchedulerRepository(t *testing.T) {
	db := &sql.DB{}
	repo := NewSchedulerRepository(db)
	if repo == nil {
		t.Fatalf("expected repo != nil")
	}
	if repo.db != db {
		t.Fatalf("expected repository to keep db reference")
	}
}

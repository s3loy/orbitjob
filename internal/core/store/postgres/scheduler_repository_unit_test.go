package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/app/schedule"
	domain "orbitjob/internal/core/domain"
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
	mock.ExpectQuery("SELECT quotas FROM tenants").WithArgs("tenant-a").WillReturnRows(sqlmock.NewRows([]string{"quotas"}).AddRow(nil))
	mock.ExpectQuery("INSERT INTO job_instances").
		WithArgs("tenant-a", int64(101), scheduledAt, 7, sqlmock.AnyArg(), 4, sqlmock.AnyArg()).
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
	mock.ExpectQuery("SELECT quotas FROM tenants").WithArgs("tenant-a").WillReturnRows(sqlmock.NewRows([]string{"quotas"}).AddRow(nil))
	mock.ExpectQuery("INSERT INTO job_instances").
		WithArgs("tenant-a", int64(101), scheduledAt, 7, sqlmock.AnyArg(), 4, sqlmock.AnyArg()).
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

// ---------------------------------------------------------------------------
// checkConcurrentInstanceQuota tests
// ---------------------------------------------------------------------------

func TestCheckConcurrentInstanceQuota_TenantNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	mock.ExpectQuery("SELECT quotas FROM tenants").
		WithArgs("tenant-missing").
		WillReturnError(sql.ErrNoRows)

	quotaExceeded, gotErr := checkConcurrentInstanceQuota(context.Background(), tx, "tenant-missing")
	if gotErr != nil {
		t.Fatalf("unexpected error: %v", gotErr)
	}
	if quotaExceeded {
		t.Fatal("expected quotaExceeded=false for missing tenant")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestCheckConcurrentInstanceQuota_NullQuotas(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	mock.ExpectQuery("SELECT quotas FROM tenants").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"quotas"}).AddRow(nil))

	quotaExceeded, gotErr := checkConcurrentInstanceQuota(context.Background(), tx, "tenant-a")
	if gotErr != nil {
		t.Fatalf("unexpected error: %v", gotErr)
	}
	if quotaExceeded {
		t.Fatal("expected quotaExceeded=false for nil quotas")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestCheckConcurrentInstanceQuota_NoQuotaKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	quotasJSON, _ := json.Marshal(map[string]any{"other_key": 10})
	mock.ExpectQuery("SELECT quotas FROM tenants").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"quotas"}).AddRow(quotasJSON))

	quotaExceeded, gotErr := checkConcurrentInstanceQuota(context.Background(), tx, "tenant-a")
	if gotErr != nil {
		t.Fatalf("unexpected error: %v", gotErr)
	}
	if quotaExceeded {
		t.Fatal("expected quotaExceeded=false when key missing")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestCheckConcurrentInstanceQuota_BelowLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	quotasJSON, _ := json.Marshal(map[string]any{"max_concurrent_instances": float64(10)})
	mock.ExpectQuery("SELECT quotas FROM tenants").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"quotas"}).AddRow(quotasJSON))

	mock.ExpectQuery("SELECT count\\(\\*\\) FROM job_instances").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	quotaExceeded, gotErr := checkConcurrentInstanceQuota(context.Background(), tx, "tenant-a")
	if gotErr != nil {
		t.Fatalf("unexpected error: %v", gotErr)
	}
	if quotaExceeded {
		t.Fatal("expected quotaExceeded=false when count < max")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestCheckConcurrentInstanceQuota_Exceeded(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	quotasJSON, _ := json.Marshal(map[string]any{"max_concurrent_instances": float64(5)})
	mock.ExpectQuery("SELECT quotas FROM tenants").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"quotas"}).AddRow(quotasJSON))

	mock.ExpectQuery("SELECT count\\(\\*\\) FROM job_instances").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	quotaExceeded, gotErr := checkConcurrentInstanceQuota(context.Background(), tx, "tenant-a")
	if gotErr != nil {
		t.Fatalf("unexpected error: %v", gotErr)
	}
	if !quotaExceeded {
		t.Fatal("expected quotaExceeded=true when count >= max")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestCheckConcurrentInstanceQuota_QuotaReadError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	mock.ExpectQuery("SELECT quotas FROM tenants").
		WithArgs("tenant-a").
		WillReturnError(errors.New("db boom"))

	_, gotErr := checkConcurrentInstanceQuota(context.Background(), tx, "tenant-a")
	if gotErr == nil || !strings.Contains(gotErr.Error(), "read tenant quotas") {
		t.Fatalf("expected read tenant quotas error, got %v", gotErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestCheckConcurrentInstanceQuota_InvalidJSON(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	mock.ExpectQuery("SELECT quotas FROM tenants").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"quotas"}).AddRow([]byte("{invalid}")))

	_, gotErr := checkConcurrentInstanceQuota(context.Background(), tx, "tenant-a")
	if gotErr == nil || !strings.Contains(gotErr.Error(), "unmarshal tenant quotas") {
		t.Fatalf("expected unmarshal tenant quotas error, got %v", gotErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestCheckConcurrentInstanceQuota_ZeroMaxConc(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	quotasJSON, _ := json.Marshal(map[string]any{"max_concurrent_instances": float64(0)})
	mock.ExpectQuery("SELECT quotas FROM tenants").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"quotas"}).AddRow(quotasJSON))

	quotaExceeded, gotErr := checkConcurrentInstanceQuota(context.Background(), tx, "tenant-a")
	if gotErr != nil {
		t.Fatalf("unexpected error: %v", gotErr)
	}
	if quotaExceeded {
		t.Fatal("expected quotaExceeded=false for zero max")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestCheckConcurrentInstanceQuota_CountError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	quotasJSON, _ := json.Marshal(map[string]any{"max_concurrent_instances": float64(10)})
	mock.ExpectQuery("SELECT quotas FROM tenants").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"quotas"}).AddRow(quotasJSON))

	mock.ExpectQuery("SELECT count\\(\\*\\) FROM job_instances").
		WithArgs("tenant-a").
		WillReturnError(errors.New("count boom"))

	_, gotErr := checkConcurrentInstanceQuota(context.Background(), tx, "tenant-a")
	if gotErr == nil || !strings.Contains(gotErr.Error(), "count concurrent instances") {
		t.Fatalf("expected count concurrent instances error, got %v", gotErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// toFloatInt tests
// ---------------------------------------------------------------------------

func TestSchedulerRepository_ScheduleOneDueCron_SetTenantContextError(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectClaimOneRow(mock, now, "tenant-a", 101, nil)
	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnError(errors.New("set_config boom"))
	mock.ExpectRollback()

	_, found, err := repo.ScheduleOneDueCron(context.Background(), now, schedule.DecideSchedule)
	if err == nil || !strings.Contains(err.Error(), "set tenant context") {
		t.Fatalf("expected set tenant context error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ScheduleOneDueCron_QuotaExceededRollbackError(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := now.Add(5 * time.Minute)
	scheduledAt := now

	mock.ExpectBegin()
	expectClaimOneRow(mock, now, "tenant-a", 101, nil)
	expectSchedulerSetTenantContext(mock, "tenant-a")
	// Quota is set to 1, but count is also 1 — exceeded
	quotasJSON, _ := json.Marshal(map[string]any{"max_concurrent_instances": float64(1)})
	mock.ExpectQuery("SELECT quotas FROM tenants").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"quotas"}).AddRow(quotasJSON))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM job_instances").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectExec("UPDATE jobs").
		WithArgs("tenant-a", int64(101), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback().WillReturnError(errors.New("rollback boom"))

	_, found, err := repo.ScheduleOneDueCron(context.Background(), now, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{CreateInstance: true, ScheduledAt: &scheduledAt, NextRunAt: &next}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "rollback on quota exceeded") {
		t.Fatalf("expected rollback on quota exceeded error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ScheduleOneDueCron_QuotaExceeded(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := now.Add(5 * time.Minute)
	scheduledAt := now

	mock.ExpectBegin()
	expectClaimOneRow(mock, now, "tenant-a", 101, nil)
	expectSchedulerSetTenantContext(mock, "tenant-a")
	// Quota is set to 2, count is 2 — exceeded
	quotasJSON, _ := json.Marshal(map[string]any{"max_concurrent_instances": float64(2)})
	mock.ExpectQuery("SELECT quotas FROM tenants").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"quotas"}).AddRow(quotasJSON))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM job_instances").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectExec("UPDATE jobs").
		WithArgs("tenant-a", int64(101), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	result, found, err := repo.ScheduleOneDueCron(context.Background(), now, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{CreateInstance: true, ScheduledAt: &scheduledAt, NextRunAt: &next}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatalf("expected found=false for quota exceeded")
	}
	if result.Created {
		t.Fatalf("expected Created=false for quota exceeded")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ScheduleOneDueCron_AuditInsertError(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := now.Add(5 * time.Minute)
	scheduledAt := now.Add(-time.Minute)
	partition := "part-1"

	mock.ExpectBegin()
	expectClaimOneRow(mock, now, "tenant-a", 101, &partition)
	expectSchedulerSetTenantContext(mock, "tenant-a")
	mock.ExpectQuery("SELECT quotas FROM tenants").
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"quotas"}).AddRow(nil))
	mock.ExpectQuery("INSERT INTO job_instances").
		WithArgs("tenant-a", int64(101), scheduledAt, 7, sqlmock.AnyArg(), 4, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"run_id"}).AddRow("run-1"))
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", "system", "scheduler", "instance.created", "instance", "run-1", sqlmock.AnyArg()).
		WillReturnError(errors.New("audit boom"))
	mock.ExpectRollback()

	_, found, err := repo.ScheduleOneDueCron(context.Background(), now, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{CreateInstance: true, ScheduledAt: &scheduledAt, NextRunAt: &next}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "insert audit event") {
		t.Fatalf("expected insert audit event error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ScheduleOneDueCron_QuotaCheckError(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := now.Add(5 * time.Minute)
	scheduledAt := now

	mock.ExpectBegin()
	expectClaimOneRow(mock, now, "tenant-a", 101, nil)
	expectSchedulerSetTenantContext(mock, "tenant-a")
	// Quotas read returns non-ErrNoRows error
	mock.ExpectQuery("SELECT quotas FROM tenants").
		WithArgs("tenant-a").
		WillReturnError(errors.New("quotas boom"))
	mock.ExpectRollback()

	_, found, err := repo.ScheduleOneDueCron(context.Background(), now, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{CreateInstance: true, ScheduledAt: &scheduledAt, NextRunAt: &next}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "read tenant quotas") {
		t.Fatalf("expected read tenant quotas error, got %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	assertMock(t, mock)
}

func TestToFloatInt(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want int
		ok   bool
	}{
		{"float64", float64(42), 42, true},
		{"float64 zero", float64(0), 0, true},
		{"float32", float32(7), 7, true},
		{"int", int(10), 10, true},
		{"int64", int64(100), 100, true},
		{"int zero", int(0), 0, true},
		{"string invalid", "abc", 0, false},
		{"bool invalid", true, 0, false},
		{"nil invalid", nil, 0, false},
		{"float64 trunc", float64(3.9), 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toFloatInt(tt.v)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("toFloatInt(%v) = (%d, %v), want (%d, %v)", tt.v, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ScheduleBatch tests
// ---------------------------------------------------------------------------

func classifySchedulerError(err error) domain.ErrorClass {
	if err == nil {
		return domain.ClassNone
	}
	if strings.Contains(err.Error(), "fatal") {
		return domain.FatalWorthy
	}
	return domain.BackoffWorthy
}

func TestScheduleBatch_NilDecide(t *testing.T) {
	repo := NewSchedulerRepository(nil)
	_, err := repo.ScheduleBatch(context.Background(), time.Now().UTC(), 1, nil, classifySchedulerError)
	if err == nil {
		t.Fatal("expected error when decide is nil")
	}
}

func TestScheduleBatch_ZeroLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	columns := []string{"id", "tenant_id", "priority", "partition_key", "retry_limit", "cron_expr", "timezone", "misfire_policy", "next_run_at"}
	rows := sqlmock.NewRows(columns)
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(now, 1).WillReturnRows(rows)
	mock.ExpectRollback()

	counts, err := repo.ScheduleBatch(context.Background(), now, 0, schedule.DecideSchedule, classifySchedulerError)
	if err != nil {
		t.Fatalf("ScheduleBatch() error = %v", err)
	}
	if counts.Handled != 0 {
		t.Fatalf("expected handled=0, got %d", counts.Handled)
	}
	assertMock(t, mock)
}

func TestScheduleBatch_BeginTxError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSchedulerRepository(db)
	mock.ExpectBegin().WillReturnError(errors.New("begin boom"))

	counts, err := repo.ScheduleBatch(context.Background(), time.Now().UTC(), 1, schedule.DecideSchedule, classifySchedulerError)
	if err != nil {
		t.Fatalf("ScheduleBatch() should not return error for classified tx error, got %v", err)
	}
	if counts.Backoff != 1 || counts.Handled != 1 {
		t.Fatalf("expected Backoff=1, Handled=1, got %+v", counts)
	}
	assertMock(t, mock)
}

func TestScheduleBatch_ClaimMultipleError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(now, 1).WillReturnError(errors.New("claim boom"))
	mock.ExpectRollback()

	counts, err := repo.ScheduleBatch(context.Background(), now, 1, schedule.DecideSchedule, classifySchedulerError)
	if err != nil {
		t.Fatalf("ScheduleBatch() should not return error for classified claim error, got %v", err)
	}
	if counts.Backoff != 1 || counts.Handled != 1 {
		t.Fatalf("expected Backoff=1, Handled=1, got %+v", counts)
	}
	assertMock(t, mock)
}

func TestScheduleBatch_NoJobs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	columns := []string{"id", "tenant_id", "priority", "partition_key", "retry_limit", "cron_expr", "timezone", "misfire_policy", "next_run_at"}
	rows := sqlmock.NewRows(columns)
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(now, 1).WillReturnRows(rows)
	mock.ExpectRollback()

	counts, err := repo.ScheduleBatch(context.Background(), now, 1, schedule.DecideSchedule, classifySchedulerError)
	if err != nil {
		t.Fatalf("ScheduleBatch() error = %v", err)
	}
	if counts.Handled != 0 {
		t.Fatalf("expected handled=0, got %d", counts.Handled)
	}
	assertMock(t, mock)
}

func TestScheduleBatch_SingleJobSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := now.Add(5 * time.Minute)

	mock.ExpectBegin()
	columns := []string{"id", "tenant_id", "priority", "partition_key", "retry_limit", "cron_expr", "timezone", "misfire_policy", "next_run_at"}
	rows := sqlmock.NewRows(columns).AddRow(101, "tenant-a", 7, nil, 3, "*/5 * * * *", "UTC", "fire_now", now.Add(-time.Minute))
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(now, 1).WillReturnRows(rows)

	mock.ExpectExec("SAVEPOINT").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SELECT set_config").WithArgs("tenant-a").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE jobs").WithArgs("tenant-a", int64(101), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("RELEASE SAVEPOINT").WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectCommit()

	counts, err := repo.ScheduleBatch(context.Background(), now, 1, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{CreateInstance: false, NextRunAt: &next}, nil
	}, classifySchedulerError)
	if err != nil {
		t.Fatalf("ScheduleBatch() error = %v", err)
	}
	if counts.Handled != 1 {
		t.Fatalf("expected handled=1, got %d", counts.Handled)
	}
	if counts.Scheduled != 0 {
		t.Fatalf("expected scheduled=0 (no instance), got %d", counts.Scheduled)
	}
	assertMock(t, mock)
}

func TestScheduleBatch_JobError_BackoffWorthy(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	columns := []string{"id", "tenant_id", "priority", "partition_key", "retry_limit", "cron_expr", "timezone", "misfire_policy", "next_run_at"}
	rows := sqlmock.NewRows(columns).AddRow(101, "tenant-a", 7, nil, 3, "*/5 * * * *", "UTC", "fire_now", now.Add(-time.Minute))
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(now, 1).WillReturnRows(rows)

	mock.ExpectExec("SAVEPOINT").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SELECT set_config").WithArgs("tenant-a").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE jobs").WithArgs("tenant-a", int64(101), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnError(errors.New("update boom"))
	mock.ExpectExec("ROLLBACK TO SAVEPOINT").WillReturnResult(sqlmock.NewResult(0, 0))
	// Job error classified as BackoffWorthy → continue, remaining jobs processed
	// No more jobs → commit
	mock.ExpectCommit()

	counts, err := repo.ScheduleBatch(context.Background(), now, 1, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		next := now.Add(5 * time.Minute)
		return schedule.ScheduleDecision{CreateInstance: false, NextRunAt: &next}, nil
	}, classifySchedulerError)
	if err != nil {
		t.Fatalf("ScheduleBatch() error = %v", err)
	}
	if counts.Backoff != 1 || counts.Handled != 1 {
		t.Fatalf("expected Backoff=1, Handled=1, got %+v", counts)
	}
	assertMock(t, mock)
}

func TestScheduleBatch_QuotaExceeded(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := now.Add(5 * time.Minute)
	scheduledAt := now

	mock.ExpectBegin()
	columns := []string{"id", "tenant_id", "priority", "partition_key", "retry_limit", "cron_expr", "timezone", "misfire_policy", "next_run_at"}
	rows := sqlmock.NewRows(columns).AddRow(101, "tenant-a", 7, nil, 3, "*/5 * * * *", "UTC", "fire_now", now.Add(-time.Minute))
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(now, 1).WillReturnRows(rows)

	mock.ExpectExec("SAVEPOINT").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SELECT set_config").WithArgs("tenant-a").WillReturnResult(sqlmock.NewResult(0, 0))

	quotasJSON, _ := json.Marshal(map[string]any{"max_concurrent_instances": float64(1)})
	mock.ExpectQuery("SELECT quotas FROM tenants").WithArgs("tenant-a").WillReturnRows(sqlmock.NewRows([]string{"quotas"}).AddRow(quotasJSON))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM job_instances").WithArgs("tenant-a").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectExec("UPDATE jobs").WithArgs("tenant-a", int64(101), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("RELEASE SAVEPOINT").WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectCommit()

	counts, err := repo.ScheduleBatch(context.Background(), now, 1, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{CreateInstance: true, ScheduledAt: &scheduledAt, NextRunAt: &next}, nil
	}, classifySchedulerError)
	if err != nil {
		t.Fatalf("ScheduleBatch() error = %v", err)
	}
	if counts.Handled != 1 {
		t.Fatalf("expected handled=1, got %d", counts.Handled)
	}
	if counts.Scheduled != 0 {
		t.Fatalf("expected scheduled=0 (quota exceeded), got %d", counts.Scheduled)
	}
	assertMock(t, mock)
}

func TestScheduleBatch_CommitError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := now.Add(5 * time.Minute)

	mock.ExpectBegin()
	columns := []string{"id", "tenant_id", "priority", "partition_key", "retry_limit", "cron_expr", "timezone", "misfire_policy", "next_run_at"}
	rows := sqlmock.NewRows(columns).AddRow(101, "tenant-a", 7, nil, 3, "*/5 * * * *", "UTC", "fire_now", now.Add(-time.Minute))
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(now, 1).WillReturnRows(rows)

	mock.ExpectExec("SAVEPOINT").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SELECT set_config").WithArgs("tenant-a").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE jobs").WithArgs("tenant-a", int64(101), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("RELEASE SAVEPOINT").WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectCommit().WillReturnError(errors.New("commit boom"))

	counts, err := repo.ScheduleBatch(context.Background(), now, 1, func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error) {
		return schedule.ScheduleDecision{CreateInstance: false, NextRunAt: &next}, nil
	}, classifySchedulerError)
	if err != nil {
		t.Fatalf("ScheduleBatch() should not return error for classified commit error, got %v", err)
	}
	// Handled=2: one from job processing + one from commit error classification
	if counts.Backoff != 1 || counts.Handled != 2 {
		t.Fatalf("expected Backoff=1, Handled=2, got %+v", counts)
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ListActiveTenantIDs_Success(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)

	rows := sqlmock.NewRows([]string{"id"}).AddRow("tenant-a").AddRow("tenant-b")
	mock.ExpectQuery("SELECT id FROM orbitjob_list_active_tenant_ids").
		WillReturnRows(rows)

	ids, err := repo.ListActiveTenantIDs(context.Background())
	if err != nil {
		t.Fatalf("ListActiveTenantIDs() error = %v", err)
	}
	if len(ids) != 2 || ids[0] != "tenant-a" || ids[1] != "tenant-b" {
		t.Fatalf("expected [tenant-a tenant-b], got %v", ids)
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ListActiveTenantIDs_Empty(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)

	rows := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery("SELECT id FROM orbitjob_list_active_tenant_ids").
		WillReturnRows(rows)

	ids, err := repo.ListActiveTenantIDs(context.Background())
	if err != nil {
		t.Fatalf("ListActiveTenantIDs() error = %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty slice, got %v", ids)
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ListActiveTenantIDs_QueryError(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)

	mock.ExpectQuery("SELECT id FROM orbitjob_list_active_tenant_ids").
		WillReturnError(errors.New("db down"))

	_, err := repo.ListActiveTenantIDs(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	assertMock(t, mock)
}

func TestSchedulerRepository_ListActiveTenantIDs_ScanError(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)

	rows := sqlmock.NewRows([]string{"id"}).AddRow(nil)
	mock.ExpectQuery("SELECT id FROM orbitjob_list_active_tenant_ids").
		WillReturnRows(rows)

	_, err := repo.ListActiveTenantIDs(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	assertMock(t, mock)
}

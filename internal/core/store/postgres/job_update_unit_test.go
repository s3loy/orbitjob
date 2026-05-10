package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/domain/resource"
	tenant "orbitjob/internal/core/domain/tenant"
)

func TestJobRepository_UpdateUnit_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	nextRun := now.Add(5 * time.Minute)
	cronExpr := "*/5 * * * *"

	spec := domainjob.UpdateSpec{
		ID:      42,
		Version: 1,
		CreateSpec: domainjob.CreateSpec{
			Name:                 "updated-job",
			TenantID:             "tenant-a",
			Priority:             9,
			PartitionKey:         strPtr("tenant-a:video"),
			TriggerType:          domainjob.TriggerTypeCron,
			CronExpr:             &cronExpr,
			Timezone:             "Asia/Shanghai",
			HandlerType:          "http",
			HandlerPayload:       map[string]any{"url": "https://example.com/hook"},
			TimeoutSec:           120,
			RetryLimit:           3,
			RetryBackoffSec:      10,
			RetryBackoffStrategy: domainjob.RetryBackoffExponential,
			ConcurrencyPolicy:    domainjob.ConcurrencyForbid,
			MisfirePolicy:        domainjob.MisfireFireNow,
			NextRunAt:            &nextRun,
		},
	}

	mock.ExpectBegin()

	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery("UPDATE jobs").
		WithArgs(
			"tenant-a", int64(42), "updated-job", 9, "tenant-a:video",
			"cron", &cronExpr, "Asia/Shanghai", "http", sqlmock.AnyArg(),
			120, 3, 10, "exponential", "forbid", "fire_now", &nextRun,
			1, // version
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "tenant_id", "status", "version", "next_run_at", "created_at", "updated_at",
		}).AddRow(int64(42), "updated-job", "tenant-a", "active", 2, nextRun, now, now))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeAPIKey, "control-plane-user", tenant.EventTypeJobUpdated, tenant.ResourceTypeJob, "42", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	out, err := repo.Update(context.Background(), spec, "control-plane-user")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if out.ID != 42 {
		t.Fatalf("expected id=42, got %d", out.ID)
	}
	if out.Name != "updated-job" {
		t.Fatalf("expected name=updated-job, got %q", out.Name)
	}
	if out.Version != 2 {
		t.Fatalf("expected version=2, got %d", out.Version)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_UpdateUnit_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)
	cronExpr := "*/5 * * * *"

	spec := domainjob.UpdateSpec{
		ID:      99,
		Version: 1,
		CreateSpec: domainjob.CreateSpec{
			TenantID:             "tenant-a",
			Name:                 "updated-job",
			Priority:             5,
			TriggerType:          domainjob.TriggerTypeManual,
			Timezone:             "UTC",
			HandlerType:          "http",
			HandlerPayload:       map[string]any{},
			TimeoutSec:           60,
			RetryLimit:           3,
			RetryBackoffSec:      10,
			RetryBackoffStrategy: domainjob.RetryBackoffFixed,
			ConcurrencyPolicy:    domainjob.ConcurrencyAllow,
			MisfirePolicy:        domainjob.MisfireSkip,
			CronExpr:             &cronExpr,
		},
	}
	spec.CronExpr = nil // manual doesn't need cron expr

	mock.ExpectBegin()

	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery("UPDATE jobs").
		WithArgs(
			"tenant-a", int64(99), "updated-job", 5, nil,
			"manual", (*string)(nil), "UTC", "http", sqlmock.AnyArg(),
			60, 3, 10, "fixed", "allow", "skip", (*time.Time)(nil),
			1,
		).
		WillReturnError(sql.ErrNoRows)

	// classifyJobWriteFailure: not found
	mock.ExpectQuery("SELECT id FROM jobs").
		WithArgs("tenant-a", int64(99)).
		WillReturnError(sql.ErrNoRows)

	_, err = repo.Update(context.Background(), spec, "control-plane-user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_UpdateUnit_Conflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)

	spec := domainjob.UpdateSpec{
		ID:      42,
		Version: 7,
		CreateSpec: domainjob.CreateSpec{
			TenantID:             "tenant-a",
			Name:                 "updated-job",
			Priority:             5,
			TriggerType:          domainjob.TriggerTypeManual,
			Timezone:             "UTC",
			HandlerType:          "http",
			HandlerPayload:       map[string]any{},
			TimeoutSec:           60,
			RetryLimit:           3,
			RetryBackoffSec:      10,
			RetryBackoffStrategy: domainjob.RetryBackoffFixed,
			ConcurrencyPolicy:    domainjob.ConcurrencyAllow,
			MisfirePolicy:        domainjob.MisfireSkip,
		},
	}

	mock.ExpectBegin()

	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery("UPDATE jobs").
		WithArgs(
			"tenant-a", int64(42), "updated-job", 5, nil,
			"manual", (*string)(nil), "UTC", "http", sqlmock.AnyArg(),
			60, 3, 10, "fixed", "allow", "skip", (*time.Time)(nil),
			7,
		).
		WillReturnError(sql.ErrNoRows)

	// classifyJobWriteFailure: job exists → version conflict
	mock.ExpectQuery("SELECT id FROM jobs").
		WithArgs("tenant-a", int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))

	_, err = repo.Update(context.Background(), spec, "control-plane-user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale version error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_UpdateUnit_BeginError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)

	spec := domainjob.UpdateSpec{
		ID:      42,
		Version: 1,
		CreateSpec: domainjob.CreateSpec{
			TenantID:             "tenant-a",
			Name:                 "updated-job",
			Priority:             5,
			TriggerType:          domainjob.TriggerTypeManual,
			Timezone:             "UTC",
			HandlerType:          "http",
			HandlerPayload:       map[string]any{},
			TimeoutSec:           60,
			RetryLimit:           3,
			RetryBackoffSec:      10,
			RetryBackoffStrategy: domainjob.RetryBackoffFixed,
			ConcurrencyPolicy:    domainjob.ConcurrencyAllow,
			MisfirePolicy:        domainjob.MisfireSkip,
		},
	}

	mock.ExpectBegin().WillReturnError(errors.New("begin boom"))

	_, err = repo.Update(context.Background(), spec, "control-plane-user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "begin job update tx") {
		t.Fatalf("expected begin job update tx error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_UpdateUnit_CommitError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	cronExpr := "*/5 * * * *"
	nextRun := now.Add(5 * time.Minute)

	spec := domainjob.UpdateSpec{
		ID:      42,
		Version: 1,
		CreateSpec: domainjob.CreateSpec{
			Name:                 "updated-job",
			TenantID:             "tenant-a",
			Priority:             5,
			TriggerType:          domainjob.TriggerTypeCron,
			CronExpr:             &cronExpr,
			Timezone:             "UTC",
			HandlerType:          "http",
			HandlerPayload:       map[string]any{},
			TimeoutSec:           60,
			RetryLimit:           3,
			RetryBackoffSec:      10,
			RetryBackoffStrategy: domainjob.RetryBackoffFixed,
			ConcurrencyPolicy:    domainjob.ConcurrencyAllow,
			MisfirePolicy:        domainjob.MisfireSkip,
			NextRunAt:            &nextRun,
		},
	}

	mock.ExpectBegin()

	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery("UPDATE jobs").
		WithArgs(
			"tenant-a", int64(42), "updated-job", 5, nil,
			"cron", &cronExpr, "UTC", "http", sqlmock.AnyArg(),
			60, 3, 10, "fixed", "allow", "skip", &nextRun,
			1,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "tenant_id", "status", "version", "next_run_at", "created_at", "updated_at",
		}).AddRow(int64(42), "updated-job", "tenant-a", "active", 2, nextRun, now, now))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeAPIKey, "control-plane-user", tenant.EventTypeJobUpdated, tenant.ResourceTypeJob, "42", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit().WillReturnError(errors.New("commit boom"))

	_, err = repo.Update(context.Background(), spec, "control-plane-user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "commit job update tx") {
		t.Fatalf("expected commit job update tx error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestClassifyJobWriteFailure_NotFound(t *testing.T) {
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

	mock.ExpectQuery("SELECT id FROM jobs").
		WithArgs("tenant-a", int64(99)).
		WillReturnError(sql.ErrNoRows)

	gotErr := classifyJobWriteFailure(context.Background(), tx, "tenant-a", 99)
	var notFoundErr *resource.NotFoundError
	if !errors.As(gotErr, &notFoundErr) {
		t.Fatalf("expected NotFoundError, got %T (%v)", gotErr, gotErr)
	}
	if notFoundErr.Resource != "job" {
		t.Fatalf("expected resource=job, got %q", notFoundErr.Resource)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestClassifyJobWriteFailure_Conflict(t *testing.T) {
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

	mock.ExpectQuery("SELECT id FROM jobs").
		WithArgs("tenant-a", int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))

	gotErr := classifyJobWriteFailure(context.Background(), tx, "tenant-a", 42)
	var conflictErr *resource.ConflictError
	if !errors.As(gotErr, &conflictErr) {
		t.Fatalf("expected ConflictError, got %T (%v)", gotErr, gotErr)
	}
	if conflictErr.Field != "version" {
		t.Fatalf("expected field=version, got %q", conflictErr.Field)
	}
	if conflictErr.Message != "stale job version" {
		t.Fatalf("expected stale job version message, got %q", conflictErr.Message)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestClassifyJobWriteFailure_QueryError(t *testing.T) {
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

	mock.ExpectQuery("SELECT id FROM jobs").
		WithArgs("tenant-a", int64(42)).
		WillReturnError(errors.New("query boom"))

	gotErr := classifyJobWriteFailure(context.Background(), tx, "tenant-a", 42)
	if gotErr == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(gotErr.Error(), "classify job write failure") {
		t.Fatalf("expected classify job write failure error, got %v", gotErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestJobRepository_UpdateUnit_AuditInsertError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewJobRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	nextRun := now.Add(5 * time.Minute)
	cronExpr := "*/5 * * * *"

	spec := domainjob.UpdateSpec{
		ID:      42,
		Version: 1,
		CreateSpec: domainjob.CreateSpec{
			Name:                 "updated-job",
			TenantID:             "tenant-a",
			Priority:             5,
			TriggerType:          domainjob.TriggerTypeCron,
			CronExpr:             &cronExpr,
			Timezone:             "UTC",
			HandlerType:          "http",
			HandlerPayload:       map[string]any{},
			TimeoutSec:           60,
			RetryLimit:           3,
			RetryBackoffSec:      10,
			RetryBackoffStrategy: domainjob.RetryBackoffFixed,
			ConcurrencyPolicy:    domainjob.ConcurrencyAllow,
			MisfirePolicy:        domainjob.MisfireSkip,
			NextRunAt:            &nextRun,
		},
	}

	mock.ExpectBegin()

	mock.ExpectExec("SELECT set_config").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery("UPDATE jobs").
		WithArgs(
			"tenant-a", int64(42), "updated-job", 5, nil,
			"cron", &cronExpr, "UTC", "http", sqlmock.AnyArg(),
			60, 3, 10, "fixed", "allow", "skip", &nextRun,
			1,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "tenant_id", "status", "version", "next_run_at", "created_at", "updated_at",
		}).AddRow(int64(42), "updated-job", "tenant-a", "active", 2, nextRun, now, now))

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("tenant-a", tenant.ActorTypeAPIKey, "control-plane-user", tenant.EventTypeJobUpdated, tenant.ResourceTypeJob, "42", sqlmock.AnyArg()).
		WillReturnError(errors.New("audit boom"))

	_, err = repo.Update(context.Background(), spec, "control-plane-user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "insert audit event") {
		t.Fatalf("expected insert audit event error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestBuildUpdateDiffPayload(t *testing.T) {
	cronExpr := "*/5 * * * *"
	in := domainjob.UpdateSpec{
		ID:      42,
		Version: 1,
		CreateSpec: domainjob.CreateSpec{
			Name:                 "nightly-report",
			TenantID:             "tenant-a",
			Priority:             9,
			PartitionKey:         strPtr("tenant-a:batch"),
			TriggerType:          domainjob.TriggerTypeCron,
			CronExpr:             &cronExpr,
			Timezone:             "Asia/Shanghai",
			HandlerType:          "http",
			HandlerPayload:       map[string]any{"url": "https://example.com/hook"},
			TimeoutSec:           120,
			RetryLimit:           3,
			RetryBackoffSec:      10,
			RetryBackoffStrategy: domainjob.RetryBackoffExponential,
			ConcurrencyPolicy:    domainjob.ConcurrencyForbid,
			MisfirePolicy:        domainjob.MisfireFireNow,
		},
	}

	payload := buildUpdateDiffPayload(in)

	if payload["from_version"] != 1 {
		t.Fatalf("expected from_version=1, got %v", payload["from_version"])
	}
	if payload["to_version"] != 2 {
		t.Fatalf("expected to_version=2, got %v", payload["to_version"])
	}
	if payload["name"] != "nightly-report" {
		t.Fatalf("expected name=nightly-report, got %v", payload["name"])
	}
	if payload["priority"] != 9 {
		t.Fatalf("expected priority=9, got %v", payload["priority"])
	}
}

func TestNormalizePayload(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		result := normalizePayload(nil)
		if result == nil {
			t.Fatal("expected non-nil empty map")
		}
		if len(result) != 0 {
			t.Fatalf("expected empty map, got %d entries", len(result))
		}
	})

	t.Run("empty map", func(t *testing.T) {
		result := normalizePayload(map[string]any{})
		if len(result) != 0 {
			t.Fatalf("expected empty map, got %d entries", len(result))
		}
	})

	t.Run("with entries", func(t *testing.T) {
		result := normalizePayload(map[string]any{
			"url":    "https://example.com",
			"method": "POST",
		})
		if len(result) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(result))
		}
		if result["url"] != "https://example.com" {
			t.Fatalf("expected url=https://example.com, got %v", result["url"])
		}
		if result["method"] != "POST" {
			t.Fatalf("expected method=POST, got %v", result["method"])
		}
	})

	t.Run("nil map zero length", func(t *testing.T) {
		var nilMap map[string]any
		result := normalizePayload(nilMap)
		if result == nil {
			t.Fatal("expected non-nil empty map for nil input")
		}
		if len(result) != 0 {
			t.Fatalf("expected empty map, got %d entries", len(result))
		}
	})
}

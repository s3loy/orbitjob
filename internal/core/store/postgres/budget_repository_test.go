package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/resource"

	"orbitjob/internal/platform/scan"
)

var budgetColumns = []string{
	"id", "tenant_id", "slo_id", "window_start", "window_end",
	"budget_total", "budget_consumed", "budget_remaining", "burn_rate",
	"status", "version",
	"evaluated_at", "last_event_at", "last_sample_at", "statistics_semantics", "evaluation_reason",
}

func newBudgetRepoMock(t *testing.T) (*BudgetRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewBudgetRepository(db), mock
}

func TestBudgetRepository_Upsert(t *testing.T) {
	repo, mock := newBudgetRepoMock(t)
	windowStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(24 * time.Hour)
	evaluatedAt := windowStart.Add(time.Hour)
	lastEventAt := evaluatedAt.Add(-time.Minute)
	lastSampleAt := lastEventAt.Add(-time.Second)
	in := slo.Budget{
		TenantID:            coreFixtureBudgetRepositoryTenantID,
		SLOID:               7,
		WindowStart:         windowStart,
		WindowEnd:           windowEnd,
		BudgetTotal:         100,
		BudgetConsumed:      12.5,
		BudgetRemaining:     87.5,
		BurnRate:            1.4,
		Status:              slo.BudgetUnevaluated,
		EvaluatedAt:         evaluatedAt,
		LastEventAt:         &lastEventAt,
		LastSampleAt:        &lastSampleAt,
		StatisticsSemantics: sli.EventTime,
		EvaluationReason:    slo.ReasonStale,
	}

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureBudgetRepositoryTenantID)
	// The upsert takes the full budget record and hands back the persisted id.
	mock.ExpectQuery("INSERT INTO budgets").
		WithArgs(
			coreFixtureBudgetRepositoryTenantID, int64(7), windowStart, windowEnd,
			100.0, 12.5, 87.5, 1.4, slo.BudgetUnevaluated,
			evaluatedAt, lastEventAt, lastSampleAt, sli.EventTime, slo.ReasonStale,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))
	mock.ExpectCommit()

	out, err := repo.Upsert(context.Background(), coreFixtureBudgetRepositoryTenantID, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ID != 42 {
		t.Errorf("id = %d, want 42", out.ID)
	}
	if out.BudgetRemaining != 87.5 {
		t.Errorf("budget_remaining = %v, want 87.5", out.BudgetRemaining)
	}
	if !out.EvaluatedAt.Equal(evaluatedAt) || out.LastEventAt == nil || !out.LastEventAt.Equal(lastEventAt) ||
		out.LastSampleAt == nil || !out.LastSampleAt.Equal(lastSampleAt) || out.StatisticsSemantics != sli.EventTime || out.EvaluationReason != slo.ReasonStale {
		t.Fatalf("measurement evidence changed during upsert: %+v", out)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetRepository_Upsert_BeginTxError(t *testing.T) {
	repo, mock := newBudgetRepoMock(t)
	mock.ExpectBegin().WillReturnError(errors.New("boom"))

	_, err := repo.Upsert(context.Background(), coreFixtureBudgetRepositoryTenantID, slo.Budget{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetRepository_Upsert_InsertError(t *testing.T) {
	repo, mock := newBudgetRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureBudgetRepositoryTenantID)
	mock.ExpectQuery("INSERT INTO budgets").
		WithArgs(coreFixtureBudgetRepositoryTenantID, int64(7), time.Time{}, time.Time{},
			0.0, 0.0, 0.0, 0.0, "", nil, nil, nil, sli.LegacyProcessing, "").
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	_, err := repo.Upsert(context.Background(), coreFixtureBudgetRepositoryTenantID, slo.Budget{SLOID: 7})
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetRepository_GetCurrent(t *testing.T) {
	repo, mock := newBudgetRepoMock(t)
	windowStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureBudgetRepositoryTenantID)
	mock.ExpectQuery("SELECT (.+) FROM budgets WHERE tenant_id = \\$1 AND slo_id = \\$2").
		WithArgs(coreFixtureBudgetRepositoryTenantID, int64(7)).
		WillReturnRows(sqlmock.NewRows(budgetColumns).AddRow(
			42, coreFixtureBudgetRepositoryTenantID, 7, windowStart, windowStart.Add(24*time.Hour),
			100, 12.5, 87.5, 1.4, "healthy", 3,
			nil, nil, nil, sli.LegacyProcessing, "",
		))
	mock.ExpectCommit()

	budget, err := repo.GetCurrent(context.Background(), coreFixtureBudgetRepositoryTenantID, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if budget.ID != 42 {
		t.Errorf("id = %d, want 42", budget.ID)
	}
	if budget.TenantID != coreFixtureBudgetRepositoryTenantID {
		t.Errorf("tenant_id = %q, want "+coreFixtureBudgetRepositoryTenantID, budget.TenantID)
	}
	if budget.SLOID != 7 {
		t.Errorf("slo_id = %d, want 7", budget.SLOID)
	}
	if budget.BudgetConsumed != 12.5 {
		t.Errorf("budget_consumed = %v, want 12.5", budget.BudgetConsumed)
	}
	if budget.Version != 3 {
		t.Errorf("version = %d, want 3", budget.Version)
	}
	if !budget.EvaluatedAt.IsZero() || budget.LastEventAt != nil || budget.LastSampleAt != nil ||
		budget.StatisticsSemantics != sli.LegacyProcessing || budget.EvaluationReason != "" {
		t.Fatalf("historical evidence must remain unspecified: %+v", budget)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetRepository_GetCurrent_NotFound(t *testing.T) {
	repo, mock := newBudgetRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureBudgetRepositoryTenantID)
	mock.ExpectQuery("SELECT (.+) FROM budgets").
		WithArgs(coreFixtureBudgetRepositoryTenantID, int64(99)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err := repo.GetCurrent(context.Background(), coreFixtureBudgetRepositoryTenantID, 99)
	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *resource.NotFoundError, got %T: %v", err, err)
	}
	if notFound.Resource != "budget" {
		t.Errorf("resource = %q, want budget", notFound.Resource)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetRepository_GetCurrent_DBError(t *testing.T) {
	repo, mock := newBudgetRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureBudgetRepositoryTenantID)
	mock.ExpectQuery("SELECT (.+) FROM budgets").
		WithArgs(coreFixtureBudgetRepositoryTenantID, int64(7)).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	_, err := repo.GetCurrent(context.Background(), coreFixtureBudgetRepositoryTenantID, 7)
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

var coreFixtureBudgetRepositoryTenantID = scan.GenerateID()

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/platform/scan"
)

// budgetRowColumns is the projection the admin budgets queries scan, in
// source order.
var budgetRowColumns = []string{
	"id", "tenant_id", "slo_id", "window_start", "window_end",
	"budget_total", "budget_consumed", "budget_remaining", "burn_rate",
	"status", "version",
	"evaluated_at", "last_event_at", "last_sample_at", "statistics_semantics", "evaluation_reason",
}

func expectTenantTx(t *testing.T, mock sqlmock.Sqlmock, tenantID string) {
	t.Helper()
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

func TestBudgetReadRepository_GetCurrent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	windowStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	evaluatedAt := windowStart.Add(time.Hour)
	lastEventAt := evaluatedAt.Add(-time.Minute)
	lastSampleAt := lastEventAt.Add(-time.Second)
	expectTenantTx(t, mock, budgetReadFixtureTenantID)
	mock.ExpectQuery(`SELECT (.+) FROM budgets WHERE tenant_id = \$1 AND slo_id = \$2`).
		WithArgs(budgetReadFixtureTenantID, int64(7), nil).
		WillReturnRows(sqlmock.NewRows(budgetRowColumns).AddRow(
			42, budgetReadFixtureTenantID, 7, windowStart, windowStart.Add(24*time.Hour),
			100.0, 12.5, 87.5, 1.4, "healthy", 3,
			evaluatedAt, lastEventAt, lastSampleAt, sli.EventTime, "",
		))
	mock.ExpectCommit()

	budget, err := NewBudgetReadRepository(db).GetCurrent(context.Background(), budgetReadFixtureTenantID, "", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if budget.ID != 42 || budget.SLOID != 7 || budget.TenantID != budgetReadFixtureTenantID {
		t.Fatalf("identity did not survive the scan: %+v", budget)
	}
	if budget.BudgetTotal != 100 || budget.BudgetConsumed != 12.5 || budget.BudgetRemaining != 87.5 {
		t.Fatalf("budget figures did not survive the scan: %+v", budget)
	}
	if budget.BurnRate != 1.4 || budget.Status != "healthy" || budget.Version != 3 {
		t.Fatalf("status fields did not survive the scan: %+v", budget)
	}
	if !budget.EvaluatedAt.Equal(evaluatedAt) || budget.LastEventAt == nil || !budget.LastEventAt.Equal(lastEventAt) ||
		budget.LastSampleAt == nil || !budget.LastSampleAt.Equal(lastSampleAt) || budget.StatisticsSemantics != sli.EventTime || budget.EvaluationReason != "" {
		t.Fatalf("measurement evidence did not survive the scan: %+v", budget)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetReadRepository_GetCurrent_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, budgetReadFixtureTenantID)
	mock.ExpectQuery(`SELECT (.+) FROM budgets`).
		WithArgs(budgetReadFixtureTenantID, int64(99), nil).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT EXISTS`).WithArgs(budgetReadFixtureTenantID, int64(99), nil).WillReturnRows(sqlmock.NewRows([]string{"visible"}).AddRow(true))
	mock.ExpectRollback()

	_, err = NewBudgetReadRepository(db).GetCurrent(context.Background(), budgetReadFixtureTenantID, "", 99)
	assertNotFound(t, err, "budget")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetReadRepository_GetCurrent_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, budgetReadFixtureTenantID)
	mock.ExpectQuery(`SELECT (.+) FROM budgets`).
		WithArgs(budgetReadFixtureTenantID, int64(7), nil).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := NewBudgetReadRepository(db).GetCurrent(context.Background(), budgetReadFixtureTenantID, "", 7); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetReadRepository_ListHistory(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	windowStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	expectTenantTx(t, mock, budgetReadFixtureTenantID)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM budgets`).
		WithArgs(budgetReadFixtureTenantID, int64(7), nil).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`SELECT (.+) FROM budgets WHERE tenant_id = \$1 AND slo_id = \$2 (.+) ORDER BY window_start DESC LIMIT \$4 OFFSET \$5`).
		WithArgs(budgetReadFixtureTenantID, int64(7), nil, 20, 0).
		WillReturnRows(sqlmock.NewRows(budgetRowColumns).
			AddRow(42, budgetReadFixtureTenantID, 7, windowStart, windowStart.Add(24*time.Hour),
				0, 0, 0, 0, slo.BudgetUnevaluated, 3, windowStart.Add(time.Hour), nil, nil, sli.EventTime, slo.ReasonNoSamples).
			AddRow(41, budgetReadFixtureTenantID, 7, windowStart.Add(-24*time.Hour), windowStart,
				100.0, 0, 100.0, 0, "healthy", 2, nil, nil, nil, sli.LegacyProcessing, ""))
	mock.ExpectCommit()

	budgets, total, err := NewBudgetReadRepository(db).ListHistory(context.Background(), budgetReadFixtureTenantID, "", 7, 20, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(budgets) != 2 {
		t.Fatalf("expected 2 budgets, got %d", len(budgets))
	}
	// The rows come back newest-window first and must land in that order.
	if budgets[0].WindowStart.Before(budgets[1].WindowStart) {
		t.Errorf("windows out of order: %v before %v", budgets[0].WindowStart, budgets[1].WindowStart)
	}
	if budgets[0].Status != slo.BudgetUnevaluated || budgets[0].EvaluationReason != slo.ReasonNoSamples ||
		!budgets[0].EvaluatedAt.Equal(windowStart.Add(time.Hour)) || budgets[0].LastEventAt != nil || budgets[0].LastSampleAt != nil ||
		budgets[0].StatisticsSemantics != sli.EventTime {
		t.Fatalf("unevaluated evidence did not survive the scan: %+v", budgets[0])
	}
	if !budgets[1].EvaluatedAt.IsZero() || budgets[1].LastEventAt != nil || budgets[1].LastSampleAt != nil ||
		budgets[1].StatisticsSemantics != sli.LegacyProcessing || budgets[1].EvaluationReason != "" {
		t.Fatalf("historical evidence must remain unspecified: %+v", budgets[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetReadRepository_ListHistory_Empty(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, budgetReadFixtureTenantID)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM budgets`).
		WithArgs(budgetReadFixtureTenantID, int64(7), nil).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT (.+) FROM budgets`).
		WithArgs(budgetReadFixtureTenantID, int64(7), nil, 20, 0).
		WillReturnRows(sqlmock.NewRows(budgetRowColumns))
	mock.ExpectCommit()

	budgets, total, err := NewBudgetReadRepository(db).ListHistory(context.Background(), budgetReadFixtureTenantID, "", 7, 20, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 || len(budgets) != 0 {
		t.Fatalf("total=%d len=%d, want 0 and 0", total, len(budgets))
	}
}

func TestBudgetReadRepository_ListHistory_CountError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, budgetReadFixtureTenantID)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM budgets`).
		WithArgs(budgetReadFixtureTenantID, int64(7), nil).
		WillReturnError(errors.New("count failed"))
	mock.ExpectRollback()

	if _, _, err := NewBudgetReadRepository(db).ListHistory(context.Background(), budgetReadFixtureTenantID, "", 7, 20, 0); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetReadRepository_ListHistory_RowsError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	windowStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	expectTenantTx(t, mock, budgetReadFixtureTenantID)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM budgets`).
		WithArgs(budgetReadFixtureTenantID, int64(7), nil).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT (.+) FROM budgets`).
		WithArgs(budgetReadFixtureTenantID, int64(7), nil, 20, 0).
		WillReturnRows(sqlmock.NewRows(budgetRowColumns).
			AddRow(42, budgetReadFixtureTenantID, 7, windowStart, windowStart.Add(24*time.Hour),
				100.0, 12.5, 87.5, 1.4, "healthy", 3, nil, nil, nil, sli.LegacyProcessing, "").
			RowError(0, errors.New("iteration failure")))
	mock.ExpectRollback()

	if _, _, err := NewBudgetReadRepository(db).ListHistory(context.Background(), budgetReadFixtureTenantID, "", 7, 20, 0); err == nil {
		t.Fatal("expected iteration error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

var budgetReadFixtureTenantID = scan.GenerateID()

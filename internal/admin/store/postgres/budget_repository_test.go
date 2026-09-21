package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// budgetRowColumns is the projection the admin budgets queries scan, in
// source order.
var budgetRowColumns = []string{
	"id", "tenant_id", "slo_id", "window_start", "window_end",
	"budget_total", "budget_consumed", "budget_remaining", "burn_rate",
	"status", "version",
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
	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM budgets WHERE tenant_id = \$1 AND slo_id = \$2`).
		WithArgs("tenant-a", int64(7)).
		WillReturnRows(sqlmock.NewRows(budgetRowColumns).AddRow(
			42, "tenant-a", 7, windowStart, windowStart.Add(24*time.Hour),
			100.0, 12.5, 87.5, 1.4, "healthy", 3,
		))
	mock.ExpectCommit()

	budget, err := NewBudgetReadRepository(db).GetCurrent(context.Background(), "tenant-a", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if budget.ID != 42 || budget.SLOID != 7 || budget.TenantID != "tenant-a" {
		t.Fatalf("identity did not survive the scan: %+v", budget)
	}
	if budget.BudgetTotal != 100 || budget.BudgetConsumed != 12.5 || budget.BudgetRemaining != 87.5 {
		t.Fatalf("budget figures did not survive the scan: %+v", budget)
	}
	if budget.BurnRate != 1.4 || budget.Status != "healthy" || budget.Version != 3 {
		t.Fatalf("status fields did not survive the scan: %+v", budget)
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

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM budgets`).
		WithArgs("tenant-a", int64(99)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err = NewBudgetReadRepository(db).GetCurrent(context.Background(), "tenant-a", 99)
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

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM budgets`).
		WithArgs("tenant-a", int64(7)).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := NewBudgetReadRepository(db).GetCurrent(context.Background(), "tenant-a", 7); err == nil {
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
	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM budgets`).
		WithArgs("tenant-a", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`SELECT (.+) FROM budgets WHERE tenant_id = \$1 AND slo_id = \$2 ORDER BY window_start DESC LIMIT \$3 OFFSET \$4`).
		WithArgs("tenant-a", int64(7), 20, 0).
		WillReturnRows(sqlmock.NewRows(budgetRowColumns).
			AddRow(42, "tenant-a", 7, windowStart, windowStart.Add(24*time.Hour),
				100.0, 12.5, 87.5, 1.4, "healthy", 3).
			AddRow(41, "tenant-a", 7, windowStart.Add(-24*time.Hour), windowStart,
				100.0, 0, 100.0, 0, "healthy", 2))
	mock.ExpectCommit()

	budgets, total, err := NewBudgetReadRepository(db).ListHistory(context.Background(), "tenant-a", 7, 20, 0)
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

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM budgets`).
		WithArgs("tenant-a", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT (.+) FROM budgets`).
		WithArgs("tenant-a", int64(7), 20, 0).
		WillReturnRows(sqlmock.NewRows(budgetRowColumns))
	mock.ExpectCommit()

	budgets, total, err := NewBudgetReadRepository(db).ListHistory(context.Background(), "tenant-a", 7, 20, 0)
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

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM budgets`).
		WithArgs("tenant-a", int64(7)).
		WillReturnError(errors.New("count failed"))
	mock.ExpectRollback()

	if _, _, err := NewBudgetReadRepository(db).ListHistory(context.Background(), "tenant-a", 7, 20, 0); err == nil {
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
	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM budgets`).
		WithArgs("tenant-a", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT (.+) FROM budgets`).
		WithArgs("tenant-a", int64(7), 20, 0).
		WillReturnRows(sqlmock.NewRows(budgetRowColumns).
			AddRow(42, "tenant-a", 7, windowStart, windowStart.Add(24*time.Hour),
				100.0, 12.5, 87.5, 1.4, "healthy", 3).
			RowError(0, errors.New("iteration failure")))
	mock.ExpectRollback()

	if _, _, err := NewBudgetReadRepository(db).ListHistory(context.Background(), "tenant-a", 7, 20, 0); err == nil {
		t.Fatal("expected iteration error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

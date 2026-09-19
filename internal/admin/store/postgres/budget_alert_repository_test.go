package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// budgetAlertRowColumns is the projection the admin budget_alerts queries
// scan, in source order.
var budgetAlertRowColumns = []string{
	"id", "tenant_id", "slo_id", "budget_id", "alert_type", "burn_rate",
	"status", "triggered_at", "resolved_at",
}

func TestBudgetAlertReadRepository_Get(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	triggered := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	resolved := time.Date(2026, 9, 1, 9, 30, 0, 0, time.UTC)
	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM budget_alerts WHERE tenant_id = \$1 AND id = \$2`).
		WithArgs("tenant-a", int64(5)).
		WillReturnRows(sqlmock.NewRows(budgetAlertRowColumns).AddRow(
			5, "tenant-a", 7, 42, "fast_burn", 14.4, "resolved", triggered, resolved,
		))
	mock.ExpectCommit()

	alert, err := NewBudgetAlertReadRepository(db).Get(context.Background(), "tenant-a", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alert.ID != 5 || alert.SLOID != 7 || alert.BudgetID != 42 {
		t.Fatalf("identity did not survive the scan: %+v", alert)
	}
	if alert.AlertType != "fast_burn" || alert.BurnRate != 14.4 || alert.Status != "resolved" {
		t.Fatalf("alert fields did not survive the scan: %+v", alert)
	}
	if alert.TriggeredAt != triggered {
		t.Errorf("triggered_at = %v, want %v", alert.TriggeredAt, triggered)
	}
	if alert.ResolvedAt == nil || *alert.ResolvedAt != resolved {
		t.Errorf("resolved_at = %v, want %v", alert.ResolvedAt, resolved)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetAlertReadRepository_Get_UnresolvedLeavesANilPointer(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM budget_alerts`).
		WithArgs("tenant-a", int64(5)).
		WillReturnRows(sqlmock.NewRows(budgetAlertRowColumns).AddRow(
			5, "tenant-a", 7, 42, "fast_burn", 14.4, "active", time.Now(), nil,
		))
	mock.ExpectCommit()

	alert, err := NewBudgetAlertReadRepository(db).Get(context.Background(), "tenant-a", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alert.ResolvedAt != nil {
		t.Errorf("resolved_at = %v, want nil for an active alert", alert.ResolvedAt)
	}
}

func TestBudgetAlertReadRepository_Get_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM budget_alerts`).
		WithArgs("tenant-a", int64(99)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err = NewBudgetAlertReadRepository(db).Get(context.Background(), "tenant-a", 99)
	assertNotFound(t, err, "budget_alert")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetAlertReadRepository_Get_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM budget_alerts`).
		WithArgs("tenant-a", int64(5)).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := NewBudgetAlertReadRepository(db).Get(context.Background(), "tenant-a", 5); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetAlertReadRepository_ListUnfiltered(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	triggered := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	expectTenantTx(t, mock, "tenant-a")
	// No slo or status filter: the count carries the tenant predicate only.
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM budget_alerts WHERE tenant_id = \$1`).
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT (.+) FROM budget_alerts WHERE tenant_id = \$1 ORDER BY triggered_at DESC LIMIT \$2 OFFSET \$3`).
		WithArgs("tenant-a", 50, 0).
		WillReturnRows(sqlmock.NewRows(budgetAlertRowColumns).AddRow(
			5, "tenant-a", 7, 42, "fast_burn", 14.4, "active", triggered, nil,
		))
	mock.ExpectCommit()

	alerts, total, err := NewBudgetAlertReadRepository(db).
		List(context.Background(), "tenant-a", nil, nil, 50, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(alerts) != 1 || alerts[0].AlertType != "fast_burn" {
		t.Fatalf("unexpected alerts: %+v", alerts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetAlertReadRepository_ListFiltered(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	sloID := int64(7)
	status := "active"
	expectTenantTx(t, mock, "tenant-a")
	// Both filters append to the count and the page, and the page gains the
	// limit/offset placeholders after them.
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM budget_alerts WHERE tenant_id = \$1 AND slo_id = \$2 AND status = \$3`).
		WithArgs("tenant-a", int64(7), "active").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT (.+) FROM budget_alerts WHERE tenant_id = \$1 AND slo_id = \$2 AND status = \$3 ORDER BY triggered_at DESC LIMIT \$4 OFFSET \$5`).
		WithArgs("tenant-a", int64(7), "active", 50, 10).
		WillReturnRows(sqlmock.NewRows(budgetAlertRowColumns))
	mock.ExpectCommit()

	alerts, total, err := NewBudgetAlertReadRepository(db).
		List(context.Background(), "tenant-a", &sloID, &status, 50, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 || len(alerts) != 0 {
		t.Fatalf("total=%d len=%d, want 0 and 0", total, len(alerts))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

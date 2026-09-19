package postgres

import (
	"context"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

var budgetAlertColumns = []string{"id", "alert_type", "status"}

func newBudgetAlertRepoMock(t *testing.T) (*BudgetAlertRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewBudgetAlertRepository(db), mock
}

func TestBudgetAlertRepository_Create(t *testing.T) {
	repo, mock := newBudgetAlertRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectExec("INSERT INTO budget_alerts").
		WithArgs("default", int64(7), int64(42), "fast_burn", 14.4).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.Create(context.Background(), "default", 7, 42, "fast_burn", 14.4); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetAlertRepository_Create_BeginTxError(t *testing.T) {
	repo, mock := newBudgetAlertRepoMock(t)
	mock.ExpectBegin().WillReturnError(errors.New("boom"))

	if err := repo.Create(context.Background(), "default", 7, 42, "fast_burn", 14.4); err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetAlertRepository_Create_InsertError(t *testing.T) {
	repo, mock := newBudgetAlertRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectExec("INSERT INTO budget_alerts").
		WithArgs("default", int64(7), int64(42), "fast_burn", 14.4).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	if err := repo.Create(context.Background(), "default", 7, 42, "fast_burn", 14.4); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetAlertRepository_ResolveBySLOAndType(t *testing.T) {
	repo, mock := newBudgetAlertRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectExec("UPDATE budget_alerts").
		WithArgs("default", int64(7), "fast_burn").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.ResolveBySLOAndType(context.Background(), "default", 7, "fast_burn"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetAlertRepository_ResolveBySLOAndType_DBError(t *testing.T) {
	repo, mock := newBudgetAlertRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectExec("UPDATE budget_alerts").
		WithArgs("default", int64(7), "fast_burn").
		WillReturnError(errors.New("update failed"))
	mock.ExpectRollback()

	if err := repo.ResolveBySLOAndType(context.Background(), "default", 7, "fast_burn"); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetAlertRepository_GetActiveBySLO(t *testing.T) {
	repo, mock := newBudgetAlertRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("SELECT id, alert_type, status FROM budget_alerts").
		WithArgs("default", int64(7)).
		WillReturnRows(sqlmock.NewRows(budgetAlertColumns).
			AddRow(1, "fast_burn", "active").
			AddRow(2, "slow_burn", "active"))
	mock.ExpectCommit()

	alerts, err := repo.GetActiveBySLO(context.Background(), "default", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(alerts) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(alerts))
	}
	if alerts[0].ID != 1 || alerts[0].AlertType != "fast_burn" {
		t.Errorf("alerts[0] = %+v, want id 1 fast_burn", alerts[0])
	}
	if alerts[1].Status != "active" {
		t.Errorf("alerts[1].status = %q, want active", alerts[1].Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBudgetAlertRepository_GetActiveBySLO_Empty(t *testing.T) {
	repo, mock := newBudgetAlertRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, "default")
	mock.ExpectQuery("SELECT id, alert_type, status FROM budget_alerts").
		WithArgs("default", int64(7)).
		WillReturnRows(sqlmock.NewRows(budgetAlertColumns))
	mock.ExpectCommit()

	alerts, err := repo.GetActiveBySLO(context.Background(), "default", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts, got %d", len(alerts))
	}
}

func TestBudgetAlertRepository_GetActiveBySLO_BeginTxError(t *testing.T) {
	repo, mock := newBudgetAlertRepoMock(t)
	mock.ExpectBegin().WillReturnError(errors.New("boom"))

	if _, err := repo.GetActiveBySLO(context.Background(), "default", 7); err == nil {
		t.Fatal("expected error")
	}
}

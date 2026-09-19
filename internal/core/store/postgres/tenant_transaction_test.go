package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// newTenantTxMock returns a mock handle for driving WithTenant and
// WithTenantTransaction directly.
func newTenantTxMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *ControlPlaneRepository) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock, NewControlPlaneRepository(db)
}

func TestWithTenant_RejectsANilTransaction(t *testing.T) {
	if err := WithTenant(context.Background(), nil, "default"); err == nil {
		t.Fatal("expected error for a nil transaction")
	}
}

func TestWithTenant_RejectsAnEmptyTenant(t *testing.T) {
	db, mock, _ := newTenantTxMock(t)
	mock.ExpectBegin()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	if err := WithTenant(context.Background(), tx, ""); err == nil {
		t.Fatal("expected error for an empty tenant id")
	}
}

func TestWithTenant_SetsTheTransactionScopedGUC(t *testing.T) {
	db, mock, _ := newTenantTxMock(t)
	mock.ExpectBegin()
	// The GUC name is bound to what the RLS policies read; the third
	// set_config argument (local = true) is a literal in the statement.
	mock.ExpectExec(`SELECT set_config\(\$1, \$2, true\)`).
		WithArgs("app.tenant_id", "tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if err := WithTenant(context.Background(), tx, "tenant-a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
	// Release the transaction only after the expectations were checked.
	_ = tx.Rollback()
}

func TestWithTenant_PropagatesTheDriverError(t *testing.T) {
	db, mock, _ := newTenantTxMock(t)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\(\$1, \$2, true\)`).
		WithArgs("app.tenant_id", "tenant-a").
		WillReturnError(errors.New("set_config failed"))
	mock.ExpectRollback()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := WithTenant(context.Background(), tx, "tenant-a"); err == nil {
		t.Fatal("expected error")
	}
}

func TestWithTenantTransaction_CommitsWhenFnSucceeds(t *testing.T) {
	_, mock, repo := newTenantTxMock(t)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\(\$1, \$2, true\)`).
		WithArgs("app.tenant_id", "tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE something").
		WithArgs("tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	ran := false
	err := repo.WithTenantTransaction(context.Background(), "tenant-a", func(_ context.Context, tx *sql.Tx) error {
		ran = true
		_, err := tx.ExecContext(context.Background(), "UPDATE something SET x = 1 WHERE tenant_id = $1", "tenant-a")
		return err
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Fatal("fn was never called")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestWithTenantTransaction_RollsBackWhenFnFails(t *testing.T) {
	_, mock, repo := newTenantTxMock(t)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\(\$1, \$2, true\)`).
		WithArgs("app.tenant_id", "tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	fnErr := errors.New("fn failed")
	err := repo.WithTenantTransaction(context.Background(), "tenant-a", func(context.Context, *sql.Tx) error {
		return fnErr
	})
	if !errors.Is(err, fnErr) {
		t.Fatalf("err = %v, want the fn error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestWithTenantTransaction_RollsBackWhenTheGUCCannotBeSet(t *testing.T) {
	_, mock, repo := newTenantTxMock(t)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\(\$1, \$2, true\)`).
		WithArgs("app.tenant_id", "tenant-a").
		WillReturnError(errors.New("set_config failed"))
	mock.ExpectRollback()

	err := repo.WithTenantTransaction(context.Background(), "tenant-a", func(context.Context, *sql.Tx) error {
		t.Fatal("fn must not run when the tenant GUC cannot be set")
		return nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestWithTenantTransaction_BeginError(t *testing.T) {
	_, mock, repo := newTenantTxMock(t)
	mock.ExpectBegin().WillReturnError(errors.New("boom"))

	if err := repo.WithTenantTransaction(context.Background(), "tenant-a", func(context.Context, *sql.Tx) error {
		t.Fatal("fn must not run when the transaction cannot begin")
		return nil
	}); err == nil {
		t.Fatal("expected error")
	}
}

func TestWithTenantTransaction_RejectsANilDatabase(t *testing.T) {
	if err := (&ControlPlaneRepository{}).WithTenantTransaction(context.Background(), "tenant-a", func(context.Context, *sql.Tx) error {
		return nil
	}); err == nil || !strings.Contains(err.Error(), "nil database") {
		t.Fatalf("err = %v, want the nil-database refusal", err)
	}

	var nilRepo *ControlPlaneRepository
	if err := nilRepo.WithTenantTransaction(context.Background(), "tenant-a", func(context.Context, *sql.Tx) error {
		return nil
	}); err == nil {
		t.Fatal("expected error for a nil repository")
	}
}

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/domain/resource"
)

func TestAPIKeyRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectExec(`INSERT INTO api_keys`).
		WithArgs("01HZX", "tenant1", "hash", "otj_abc123").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Create(context.Background(), "tenant1", "01HZX", "hash", "otj_abc123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAPIKeyRepository_Create_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectExec(`INSERT INTO api_keys`).
		WithArgs("01HZX", "tenant1", "hash", "otj_abc123").
		WillReturnError(errors.New("db down"))

	if err := repo.Create(context.Background(), "tenant1", "01HZX", "hash", "otj_abc123"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAPIKeyRepository_ListByTenant(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	createdAt := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT id, key_prefix, created_at, revoked_at FROM api_keys`).
		WithArgs("tenant1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "key_prefix", "created_at", "revoked_at"}).
			AddRow("01HZX", "otj_abc123", createdAt, nil))

	out, err := repo.ListByTenant(context.Background(), "tenant1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out))
	}
	if out[0].ID != "01HZX" {
		t.Fatalf("expected id=%q, got %q", "01HZX", out[0].ID)
	}
	if out[0].KeyPrefix != "otj_abc123" {
		t.Fatalf("expected prefix=%q, got %q", "otj_abc123", out[0].KeyPrefix)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAPIKeyRepository_ListByTenant_EmptyResult(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectQuery(`SELECT id, key_prefix, created_at, revoked_at FROM api_keys`).
		WithArgs("tenant1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "key_prefix", "created_at", "revoked_at"}))

	out, err := repo.ListByTenant(context.Background(), "tenant1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(out) != 0 {
		t.Fatalf("expected 0 items, got %d", len(out))
	}
}

func TestAPIKeyRepository_ListByTenant_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectQuery(`SELECT id, key_prefix, created_at, revoked_at FROM api_keys`).
		WillReturnError(errors.New("db down"))

	if _, err := repo.ListByTenant(context.Background(), "tenant1"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAPIKeyRepository_ListByTenant_RowsError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	createdAt := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"id", "key_prefix", "created_at", "revoked_at"}).
		AddRow("01HZX", "otj_abc123", createdAt, nil).
		RowError(0, errors.New("iteration failure"))
	mock.ExpectQuery(`SELECT id, key_prefix, created_at, revoked_at FROM api_keys`).
		WithArgs("tenant1").
		WillReturnRows(rows)

	if _, err := repo.ListByTenant(context.Background(), "tenant1"); err == nil {
		t.Fatal("expected rows iteration error")
	}
}

func TestAPIKeyRepository_ListByTenant_ScanError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	rows := sqlmock.NewRows([]string{"id", "key_prefix", "created_at", "revoked_at"}).
		AddRow("01HZX", "otj_abc123", "not-a-time", nil)
	mock.ExpectQuery(`SELECT id, key_prefix, created_at, revoked_at FROM api_keys`).
		WithArgs("tenant1").
		WillReturnRows(rows)

	if _, err := repo.ListByTenant(context.Background(), "tenant1"); err == nil {
		t.Fatal("expected scan error")
	}
}

func TestAPIKeyRepository_Revoke(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectExec(`UPDATE api_keys SET revoked_at = \$1`).
		WithArgs(sqlmock.AnyArg(), "01HZX", "tenant1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Revoke(context.Background(), "tenant1", "01HZX"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAPIKeyRepository_Revoke_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectExec(`UPDATE api_keys SET revoked_at = \$1`).
		WithArgs(sqlmock.AnyArg(), "01HZX", "tenant1").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = repo.Revoke(context.Background(), "tenant1", "01HZX")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *resource.NotFoundError, got %T", err)
	}
	if notFound.Resource != "api_key" {
		t.Fatalf("expected resource=%q, got %q", "api_key", notFound.Resource)
	}
	if notFound.ID != "01HZX" {
		t.Fatalf("expected id=%q, got %q", "01HZX", notFound.ID)
	}
}

func TestAPIKeyRepository_Revoke_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectExec(`UPDATE api_keys SET revoked_at = \$1`).
		WithArgs(sqlmock.AnyArg(), "01HZX", "tenant1").
		WillReturnError(errors.New("db down"))

	if err := repo.Revoke(context.Background(), "tenant1", "01HZX"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAPIKeyRepository_Revoke_RowsAffectedError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectExec(`UPDATE api_keys SET revoked_at = \$1`).
		WithArgs(sqlmock.AnyArg(), "01HZX", "tenant1").
		WillReturnResult(sqlmock.NewErrorResult(errors.New("rows affected failed")))

	if err := repo.Revoke(context.Background(), "tenant1", "01HZX"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/tenant"
	"orbitjob/internal/domain/resource"
)

func TestTenantRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewTenantRepository(db)
	mock.ExpectExec(`INSERT INTO tenants`).
		WithArgs("01HZX", "acme", "Acme Corp", "active").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Create(context.Background(), &tenant.Tenant{
		ID:     "01HZX",
		Slug:   "acme",
		Name:   "Acme Corp",
		Status: "active",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestTenantRepository_Create_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewTenantRepository(db)
	mock.ExpectExec(`INSERT INTO tenants`).
		WithArgs("01HZX", "acme", "Acme Corp", "active").
		WillReturnError(errors.New("db down"))

	if err := repo.Create(context.Background(), &tenant.Tenant{
		ID:     "01HZX",
		Slug:   "acme",
		Name:   "Acme Corp",
		Status: "active",
	}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTenantRepository_List(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewTenantRepository(db)
	mock.ExpectQuery(`SELECT id, slug, name, status FROM tenants`).
		WithArgs(10, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "slug", "name", "status"}).
			AddRow("01HZX", "acme", "Acme Corp", "active"))

	out, err := repo.List(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out))
	}
	if out[0].Slug != "acme" {
		t.Fatalf("expected slug=%q, got %q", "acme", out[0].Slug)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestTenantRepository_List_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewTenantRepository(db)
	mock.ExpectQuery(`SELECT id, slug, name, status FROM tenants`).
		WillReturnError(errors.New("db down"))

	if _, err := repo.List(context.Background(), 10, 0); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTenantRepository_Get(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewTenantRepository(db)
	mock.ExpectQuery(`SELECT id, slug, name, status FROM tenants WHERE id = \$1`).
		WithArgs("01HZX").
		WillReturnRows(sqlmock.NewRows([]string{"id", "slug", "name", "status"}).
			AddRow("01HZX", "acme", "Acme Corp", "active"))

	out, err := repo.Get(context.Background(), "01HZX")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ID != "01HZX" {
		t.Fatalf("expected id=%q, got %q", "01HZX", out.ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestTenantRepository_Get_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewTenantRepository(db)
	mock.ExpectQuery(`SELECT id, slug, name, status FROM tenants WHERE id = \$1`).
		WithArgs("01HZX").
		WillReturnError(sql.ErrNoRows)

	_, err = repo.Get(context.Background(), "01HZX")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *resource.NotFoundError, got %T", err)
	}
	if notFound.Resource != "tenant" {
		t.Fatalf("expected resource=%q, got %q", "tenant", notFound.Resource)
	}
	if notFound.ID != "01HZX" {
		t.Fatalf("expected id=%q, got %q", "01HZX", notFound.ID)
	}
}

func TestTenantRepository_Get_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewTenantRepository(db)
	mock.ExpectQuery(`SELECT id, slug, name, status FROM tenants WHERE id = \$1`).
		WithArgs("01HZX").
		WillReturnError(errors.New("db down"))

	if _, err := repo.Get(context.Background(), "01HZX"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTenantRepository_List_EmptyResult(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewTenantRepository(db)
	mock.ExpectQuery(`SELECT id, slug, name, status FROM tenants`).
		WithArgs(10, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "slug", "name", "status"}))

	out, err := repo.List(context.Background(), 10, 0)
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

func TestTenantRepository_List_RowsError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewTenantRepository(db)
	rows := sqlmock.NewRows([]string{"id", "slug", "name", "status"}).
		AddRow("01HZX", "acme", "Acme Corp", "active").
		RowError(0, errors.New("iteration failure"))
	mock.ExpectQuery(`SELECT id, slug, name, status FROM tenants`).
		WithArgs(10, 0).
		WillReturnRows(rows)

	if _, err := repo.List(context.Background(), 10, 0); err == nil {
		t.Fatal("expected rows iteration error")
	}
}

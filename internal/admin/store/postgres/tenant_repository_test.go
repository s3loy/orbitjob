package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"orbitjob/internal/core/domain/tenant"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/scan"
)

func TestTenantRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewTenantRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantRepoSubjectID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO tenants`).
		WithArgs(tenantRepoSubjectID, "acme", "Acme Corp", "active").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.Create(context.Background(), &tenant.Tenant{
		ID:     tenantRepoSubjectID,
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
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantRepoSubjectID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO tenants`).
		WithArgs(tenantRepoSubjectID, "acme", "Acme Corp", "active").
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if err := repo.Create(context.Background(), &tenant.Tenant{
		ID:     tenantRepoSubjectID,
		Slug:   "acme",
		Name:   "Acme Corp",
		Status: "active",
	}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTenantRepository_Create_DuplicateSlug(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewTenantRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantRepoSubjectID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO tenants`).
		WithArgs(tenantRepoSubjectID, "acme", "Acme Corp", "active").
		WillReturnError(&pq.Error{Code: "23505", Message: `duplicate key value violates unique constraint "uniq_tenants_slug"`})
	mock.ExpectRollback()

	err = repo.Create(context.Background(), &tenant.Tenant{
		ID:     tenantRepoSubjectID,
		Slug:   "acme",
		Name:   "Acme Corp",
		Status: "active",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *resource.ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *resource.ConflictError, got %T: %v", err, err)
	}
	if ce.Resource != "tenant" {
		t.Errorf("expected resource 'tenant', got %q", ce.Resource)
	}
	if ce.Field != "slug" {
		t.Errorf("expected field 'slug', got %q", ce.Field)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTenantRepository_List(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewTenantRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantRepoCallerID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id, slug, name, status FROM tenants`).
		WithArgs(10, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "slug", "name", "status"}).
			AddRow(tenantRepoSubjectID, "acme", "Acme Corp", "active"))
	mock.ExpectCommit()

	out, err := repo.List(context.Background(), tenantRepoCallerID, 10, 0)
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
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantRepoCallerID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id, slug, name, status FROM tenants`).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := repo.List(context.Background(), tenantRepoCallerID, 10, 0); err == nil {
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
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantRepoCallerID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id, slug, name, status FROM tenants WHERE id = \$1`).
		WithArgs(tenantRepoSubjectID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "slug", "name", "status"}).
			AddRow(tenantRepoSubjectID, "acme", "Acme Corp", "active"))
	mock.ExpectCommit()

	out, err := repo.Get(context.Background(), tenantRepoCallerID, tenantRepoSubjectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ID != tenantRepoSubjectID {
		t.Fatalf("expected id=%q, got %q", tenantRepoSubjectID, out.ID)
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
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantRepoCallerID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id, slug, name, status FROM tenants WHERE id = \$1`).
		WithArgs(tenantRepoSubjectID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err = repo.Get(context.Background(), tenantRepoCallerID, tenantRepoSubjectID)
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
	if notFound.ID != tenantRepoSubjectID {
		t.Fatalf("expected id=%q, got %q", tenantRepoSubjectID, notFound.ID)
	}
}

func TestTenantRepository_Get_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewTenantRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantRepoCallerID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id, slug, name, status FROM tenants WHERE id = \$1`).
		WithArgs(tenantRepoSubjectID).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := repo.Get(context.Background(), tenantRepoCallerID, tenantRepoSubjectID); err == nil {
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
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantRepoCallerID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id, slug, name, status FROM tenants`).
		WithArgs(10, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "slug", "name", "status"}))
	mock.ExpectCommit()

	out, err := repo.List(context.Background(), tenantRepoCallerID, 10, 0)
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
		AddRow(tenantRepoSubjectID, "acme", "Acme Corp", "active").
		RowError(0, errors.New("iteration failure"))
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantRepoCallerID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id, slug, name, status FROM tenants`).
		WithArgs(10, 0).
		WillReturnRows(rows)
	mock.ExpectRollback()

	if _, err := repo.List(context.Background(), tenantRepoCallerID, 10, 0); err == nil {
		t.Fatal("expected rows iteration error")
	}
}

var tenantRepoSubjectID = scan.GenerateID()

var tenantRepoCallerID = scan.GenerateID()

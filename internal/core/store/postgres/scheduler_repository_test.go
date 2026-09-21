package postgres

import (
	"context"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func newSchedulerRepoMock(t *testing.T) (*SchedulerRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewSchedulerRepository(db), mock
}

func TestSchedulerRepository_ListActiveTenantIDs(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)

	mock.ExpectQuery("SELECT id FROM orbitjob_list_active_tenant_ids\\(\\)").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("tenant-a").AddRow("tenant-b"))

	ids, err := repo.ListActiveTenantIDs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 ids, got %d", len(ids))
	}
	if ids[0] != "tenant-a" || ids[1] != "tenant-b" {
		t.Errorf("ids = %v, want [tenant-a tenant-b]", ids)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSchedulerRepository_ListActiveTenantIDs_Empty(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)

	mock.ExpectQuery("SELECT id FROM orbitjob_list_active_tenant_ids\\(\\)").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	ids, err := repo.ListActiveTenantIDs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ids == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 ids, got %d", len(ids))
	}
}

func TestSchedulerRepository_ListActiveTenantIDs_DBError(t *testing.T) {
	repo, mock := newSchedulerRepoMock(t)

	mock.ExpectQuery("SELECT id FROM orbitjob_list_active_tenant_ids\\(\\)").
		WillReturnError(errors.New("db down"))

	if _, err := repo.ListActiveTenantIDs(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

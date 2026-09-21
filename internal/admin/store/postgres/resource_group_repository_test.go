package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/resourcegroup"
)

func TestResourceGroupRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	group := resourcegroup.Group{ID: "rg-1", TenantID: "tenant-a", Slug: "ci", Name: "CI"}
	ev := audit.Event{
		TenantID: "tenant-a", ActorID: "caller-key", EventType: audit.EventCreate,
		ResourceType: "resource_group", ResourceID: "rg-1",
	}

	expectTenantTx(t, mock, "tenant-a")
	// Slug uniqueness is per tenant, so the pair is stored unchecked here and
	// a duplicate surfaces as a database conflict.
	mock.ExpectExec(`INSERT INTO resource_groups`).
		WithArgs("rg-1", "tenant-a", "ci", "CI").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO audit_events`).
		WithArgs("tenant-a", "caller-key", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := NewResourceGroupRepository(db).Create(context.Background(), group, ev); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestResourceGroupRepository_CreateReportsADuplicateSlug(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectExec(`INSERT INTO resource_groups`).
		WithArgs("rg-2", "tenant-a", "ci", "CI").
		WillReturnError(uniqueViolation())
	mock.ExpectRollback()

	group := resourcegroup.Group{ID: "rg-2", TenantID: "tenant-a", Slug: "ci", Name: "CI"}
	err = NewResourceGroupRepository(db).Create(context.Background(), group, testAuditEvent())
	assertConflict(t, err, "resource_group", "slug")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestResourceGroupRepository_Create_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectExec(`INSERT INTO resource_groups`).
		WithArgs("rg-1", "tenant-a", "ci", "CI").
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	group := resourcegroup.Group{ID: "rg-1", TenantID: "tenant-a", Slug: "ci", Name: "CI"}
	if err := NewResourceGroupRepository(db).Create(context.Background(), group, testAuditEvent()); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestResourceGroupRepository_List(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM resource_groups WHERE tenant_id = \$1 ORDER BY slug`).
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "slug", "name"}).
			AddRow("rg-1", "tenant-a", "ci", "CI").
			AddRow("rg-2", "tenant-a", "web", "Web"))
	// The read runs in the caller's transaction and releases it; there is no
	// commit to expect on a read.
	mock.ExpectRollback()

	groups, err := NewResourceGroupRepository(db).List(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].Slug != "ci" || groups[1].Slug != "web" {
		t.Fatalf("groups out of order: %+v", groups)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestResourceGroupRepository_Exists(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM resource_groups WHERE id = \$1 AND tenant_id = \$2\)`).
		WithArgs("rg-1", "tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()

	exists, err := NewResourceGroupRepository(db).Exists(context.Background(), "tenant-a", "rg-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("exists = false for the tenant's own group")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestResourceGroupRepository_Exists_ForeignGroup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	// A group id from another tenant must read as absent, so a key cannot be
	// scoped to a group the caller never had.
	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM resource_groups`).
		WithArgs("rg-other", "tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	exists, err := NewResourceGroupRepository(db).Exists(context.Background(), "tenant-a", "rg-other")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Fatal("exists = true for a foreign group id")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

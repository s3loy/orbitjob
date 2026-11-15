package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/scan"
)

func TestAPIKeyRepository_Create(t *testing.T) {
	tenantID, keyID := scan.GenerateID(), scan.GenerateID()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	// kind is a literal in the statement; group, boundary and created_by are
	// NULL when unset, which is what an ungrouped, unbounded key looks like.
	mock.ExpectExec(`INSERT INTO api_keys`).
		WithArgs(keyID, tenantID, nil, nil, "hash", "otj_abc123", nil, "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO audit_events`).WithArgs(tenantID, sqlmock.AnyArg(), audit.EventCreate, audit.ResourceAPIKey, keyID, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := repo.Create(context.Background(), apikey.PersistInput{TenantID: tenantID, ID: keyID, KeyHash: "hash", KeyPrefix: "otj_abc123"}, testKeyEvent(tenantID, keyID, audit.EventCreate)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAPIKeyRepository_Create_DBError(t *testing.T) {
	tenantID, keyID := scan.GenerateID(), scan.GenerateID()

	dbDown := errors.New("db down")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO api_keys`).
		WithArgs(keyID, tenantID, nil, nil, "hash", "otj_abc123", nil, "").
		WillReturnError(dbDown)
	mock.ExpectRollback()

	if err := repo.Create(context.Background(), apikey.PersistInput{TenantID: tenantID, ID: keyID, KeyHash: "hash", KeyPrefix: "otj_abc123"}, testKeyEvent(tenantID, keyID, audit.EventCreate)); !errors.Is(err, dbDown) {
		t.Fatalf("expected database sentinel, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAPIKeyRepository_ListByTenant(t *testing.T) {
	tenantID, keyID := scan.GenerateID(), scan.GenerateID()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	createdAt := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id, key_prefix, created_at, revoked_at, name FROM api_keys`).
		WithArgs(tenantID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "key_prefix", "created_at", "revoked_at", "name"}).
			AddRow(keyID, "otj_abc123", createdAt, nil, "Daily operator"))
	mock.ExpectCommit()

	out, err := repo.ListByTenant(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out))
	}
	if out[0].ID != keyID {
		t.Fatalf("expected id=%q, got %q", keyID, out[0].ID)
	}
	if out[0].KeyPrefix != "otj_abc123" {
		t.Fatalf("expected prefix=%q, got %q", "otj_abc123", out[0].KeyPrefix)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAPIKeyRepository_ListByTenant_EmptyResult(t *testing.T) {
	tenantID := scan.GenerateID()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id, key_prefix, created_at, revoked_at, name FROM api_keys`).
		WithArgs(tenantID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "key_prefix", "created_at", "revoked_at", "name"}))
	mock.ExpectCommit()

	out, err := repo.ListByTenant(context.Background(), tenantID)
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
	tenantID := scan.GenerateID()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id, key_prefix, created_at, revoked_at, name FROM api_keys`).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := repo.ListByTenant(context.Background(), tenantID); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAPIKeyRepository_ListByTenant_RowsError(t *testing.T) {
	tenantID, keyID := scan.GenerateID(), scan.GenerateID()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	createdAt := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"id", "key_prefix", "created_at", "revoked_at", "name"}).
		AddRow(keyID, "otj_abc123", createdAt, nil, "Daily operator").
		RowError(0, errors.New("iteration failure"))
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id, key_prefix, created_at, revoked_at, name FROM api_keys`).
		WithArgs(tenantID).
		WillReturnRows(rows)
	mock.ExpectRollback()

	if _, err := repo.ListByTenant(context.Background(), tenantID); err == nil {
		t.Fatal("expected rows iteration error")
	}
}

func TestAPIKeyRepository_ListByTenant_ScanError(t *testing.T) {
	tenantID, keyID := scan.GenerateID(), scan.GenerateID()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	rows := sqlmock.NewRows([]string{"id", "key_prefix", "created_at", "revoked_at", "name"}).
		AddRow(keyID, "otj_abc123", "not-a-time", nil, "Daily operator")
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id, key_prefix, created_at, revoked_at, name FROM api_keys`).
		WithArgs(tenantID).
		WillReturnRows(rows)
	mock.ExpectRollback()

	if _, err := repo.ListByTenant(context.Background(), tenantID); err == nil {
		t.Fatal("expected scan error")
	}
}

func TestAPIKeyRepository_Revoke(t *testing.T) {
	tenantID, keyID := scan.GenerateID(), scan.GenerateID()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`UPDATE api_keys SET revoked_at = \$1`).
		WithArgs(sqlmock.AnyArg(), keyID, tenantID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO audit_events`).WithArgs(tenantID, sqlmock.AnyArg(), audit.EventRevoke, audit.ResourceAPIKey, keyID, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := repo.Revoke(context.Background(), tenantID, keyID, testKeyEvent(tenantID, keyID, audit.EventRevoke)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAPIKeyRepository_Revoke_NotFound(t *testing.T) {
	tenantID, keyID := scan.GenerateID(), scan.GenerateID()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`UPDATE api_keys SET revoked_at = \$1`).
		WithArgs(sqlmock.AnyArg(), keyID, tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err = repo.Revoke(context.Background(), tenantID, keyID, testKeyEvent(tenantID, keyID, audit.EventRevoke))
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
	if notFound.ID != keyID {
		t.Fatalf("expected id=%q, got %q", keyID, notFound.ID)
	}
}

func TestAPIKeyRepository_Revoke_DBError(t *testing.T) {
	tenantID, keyID := scan.GenerateID(), scan.GenerateID()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`UPDATE api_keys SET revoked_at = \$1`).
		WithArgs(sqlmock.AnyArg(), keyID, tenantID).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if err := repo.Revoke(context.Background(), tenantID, keyID, testKeyEvent(tenantID, keyID, audit.EventRevoke)); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAPIKeyRepository_Revoke_RowsAffectedError(t *testing.T) {
	tenantID, keyID := scan.GenerateID(), scan.GenerateID()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app\.tenant_id', \$1, true\)`).
		WithArgs(tenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`UPDATE api_keys SET revoked_at = \$1`).
		WithArgs(sqlmock.AnyArg(), keyID, tenantID).
		WillReturnResult(sqlmock.NewErrorResult(errors.New("rows affected failed")))
	mock.ExpectRollback()

	if err := repo.Revoke(context.Background(), tenantID, keyID, testKeyEvent(tenantID, keyID, audit.EventRevoke)); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAPIKeyRepository_FindKeyTenant(t *testing.T) {
	keyID, tenantID := scan.GenerateID(), scan.GenerateID()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	rows := sqlmock.NewRows([]string{"tenant_id"}).AddRow(tenantID)
	mock.ExpectQuery(`SELECT tenant_id FROM orbitjob_find_key_tenant\(\$1\)`).
		WithArgs(keyID).
		WillReturnRows(rows)

	actualTenant, err := repo.FindKeyTenant(context.Background(), keyID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if actualTenant != tenantID {
		t.Errorf("expected tenant %q, got %q", tenantID, actualTenant)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestAPIKeyRepository_FindKeyTenant_NotFound(t *testing.T) {
	keyID := scan.GenerateID()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewAPIKeyRepository(db)
	mock.ExpectQuery(`SELECT tenant_id FROM orbitjob_find_key_tenant\(\$1\)`).
		WithArgs(keyID).
		WillReturnError(sql.ErrNoRows)

	_, err = repo.FindKeyTenant(context.Background(), keyID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var nf *resource.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("expected *resource.NotFoundError, got %T: %v", err, err)
	}
	if nf.Resource != "api_key" {
		t.Errorf("expected resource 'api_key', got %q", nf.Resource)
	}
}

func TestAPIKeyRepository_Scope(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	key, tenant, group := scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
	mock.ExpectQuery(`SELECT tenant_id FROM orbitjob_find_key_tenant`).WithArgs(key).WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow(tenant))
	expectTenantTx(t, mock, tenant)
	mock.ExpectQuery(`SELECT COALESCE`).WithArgs(tenant, key).WillReturnRows(sqlmock.NewRows([]string{"group"}).AddRow(group))
	mock.ExpectCommit()
	got, err := NewAPIKeyRepository(db).Scope(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != key || got.TenantID != tenant || got.ResourceGroupID != group {
		t.Fatalf("wrong scope: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func testKeyEvent(tenantID, keyID, eventType string) audit.Event {
	return audit.Event{TenantID: tenantID, ActorID: scan.GenerateID(), ResourceType: audit.ResourceAPIKey, ResourceID: keyID, EventType: eventType}
}

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/policy"
)

const policyDoc = `{"version":"1","statement":[{"effect":"Allow","action":["job:Create"],"resource":["orbitjob:self:*:job/*"]}]}`

func TestPolicyRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WithArgs("tenant-a").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO policies`).
		WithArgs("p1", "tenant-a", "JobOperator", "runs jobs", jsonBytes{want: policyDoc}).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO audit_events`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err = NewPolicyRepository(db).Create(context.Background(), policy.Record{
		ID: "p1", TenantID: "tenant-a", Name: "JobOperator", Description: "runs jobs",
	}, []byte(policyDoc), testAuditEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// A duplicate name is a conflict the caller can act on, not an internal error:
// the API maps it to 409 rather than 500.
func TestPolicyRepository_CreateReportsANameConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO policies`).WillReturnError(uniqueViolation())
	mock.ExpectRollback()

	err = NewPolicyRepository(db).Create(context.Background(), policy.Record{
		ID: "p1", TenantID: "tenant-a", Name: "JobOperator",
	}, []byte(policyDoc), testAuditEvent())
	assertConflict(t, err, "policy", "name")
}

// Deleting a platform preset is refused for every caller. The owner has to be
// read first, because an empty DELETE result looks the same whether the policy
// is missing or protected.
func TestPolicyRepository_DeleteRefusesAPlatformPreset(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT tenant_id FROM policies`).
		WithArgs("preset-1", "tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow(nil))
	mock.ExpectRollback()

	err = NewPolicyRepository(db).Delete(context.Background(), "tenant-a", "preset-1", testAuditEvent())
	if !errors.Is(err, policy.ErrPlatformPolicyImmutable) {
		t.Fatalf("expected the platform-preset refusal, got %v", err)
	}
}

func TestPolicyRepository_DeleteReportsNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT tenant_id FROM policies`).
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}))
	mock.ExpectRollback()

	err = NewPolicyRepository(db).Delete(context.Background(), "tenant-a", "missing", testAuditEvent())
	assertNotFound(t, err, "policy")
}

func TestPolicyRepository_DeleteRemovesTheTenantsOwnPolicy(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT tenant_id FROM policies`).
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow("tenant-a"))
	mock.ExpectExec(`DELETE FROM policies`).
		WithArgs("p1", "tenant-a").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO audit_events`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := NewPolicyRepository(db).Delete(context.Background(), "tenant-a", "p1", testAuditEvent()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A policy still bound to a key is refused rather than cascaded: removing the
// binding silently would shrink what live keys can do.
func TestPolicyRepository_DeleteRefusesABoundPolicy(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT tenant_id FROM policies`).
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow("tenant-a"))
	mock.ExpectExec(`DELETE FROM policies`).WillReturnError(foreignKeyViolation())
	mock.ExpectRollback()

	err = NewPolicyRepository(db).Delete(context.Background(), "tenant-a", "p1", testAuditEvent())
	assertConflict(t, err, "policy", "id")
}

func TestPolicyRepository_ListReturnsOwnAndPlatformPolicies(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM policies`).
		WithArgs("tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name", "description", "document"}).
			AddRow("preset", nil, "ReadOnlyAccess", "", []byte(policyDoc)).
			AddRow("own", "tenant-a", "JobOperator", "", []byte(policyDoc)))
	mock.ExpectRollback()

	records, err := NewPolicyRepository(db).List(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if !records[0].IsPlatformPreset() {
		t.Error("a NULL owner was not read back as a platform preset")
	}
	if records[1].IsPlatformPreset() {
		t.Error("a tenant-owned policy was read back as a preset")
	}
}

// A stored document that no longer parses is a corrupt row, and reporting it is
// better than returning an empty policy that silently grants nothing.
func TestPolicyRepository_ListRejectsACorruptDocument(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM policies`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name", "description", "document"}).
			AddRow("broken", "tenant-a", "Broken", "", []byte(`not json`)))
	mock.ExpectRollback()

	if _, err := NewPolicyRepository(db).List(context.Background(), "tenant-a"); err == nil {
		t.Fatal("expected an unparsable stored document to be reported")
	}
}

// A policy outside the tenant and a policy that does not exist both leave the
// scoped query empty, and both must surface as NotFoundError: the API maps
// that to 404, while a raw sql.ErrNoRows would leak out as a 500.
func TestPolicyRepository_GetReportsNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	// The scoped predicate makes a cross-tenant read empty; the store cannot
	// tell "missing" from "another tenant's" and must not try.
	mock.ExpectQuery(`WHERE id = \$1 AND \(tenant_id = \$2 OR tenant_id IS NULL\)`).
		WithArgs("p1", "tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name", "description", "document"}))
	mock.ExpectRollback()

	_, err = NewPolicyRepository(db).Get(context.Background(), "tenant-a", "p1")
	assertNotFound(t, err, "policy")
}

func TestPolicyRepository_GetScopesToTheTenantOrAPreset(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	// The application-level predicate is the point: RLS is a second line of
	// defence, absent in the test schema and bypassed by a superuser.
	mock.ExpectQuery(`WHERE id = \$1 AND \(tenant_id = \$2 OR tenant_id IS NULL\)`).
		WithArgs("p1", "tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name", "description", "document"}).
			AddRow("p1", "tenant-a", "JobOperator", "", []byte(policyDoc)))
	mock.ExpectRollback()

	rec, err := NewPolicyRepository(db).Get(context.Background(), "tenant-a", "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Name != "JobOperator" {
		t.Fatalf("unexpected record: %+v", rec)
	}
}

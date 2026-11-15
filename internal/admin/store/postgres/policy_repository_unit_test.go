package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/policy"

	"orbitjob/internal/platform/scan"
)

const policyDoc = `{"version":"1","statement":[{"effect":"Allow","action":["job:Create"],"resource":["orbitjob:self:*:job/*"]}]}`

func TestPolicyRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WithArgs(policyStoreFixtureTenantID).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO policies`).
		WithArgs(policyStoreFixtureSubjectID, policyStoreFixtureTenantID, "JobOperator", "runs jobs", jsonBytes{want: policyDoc}).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO audit_events`).WithArgs(policyStoreFixtureTenantID, policyStoreFixtureActorID, audit.EventCreate, audit.ResourcePolicy, policyStoreFixtureSubjectID, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err = NewPolicyRepository(db).Create(context.Background(), policy.Record{
		ID: policyStoreFixtureSubjectID, TenantID: policyStoreFixtureTenantID, Name: "JobOperator", Description: "runs jobs",
	}, []byte(policyDoc), testAuditEvent(policyStoreFixtureTenantID, policyStoreFixtureActorID, audit.EventCreate, audit.ResourcePolicy, policyStoreFixtureSubjectID))
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
		ID: policyStoreFixtureSubjectID, TenantID: policyStoreFixtureTenantID, Name: "JobOperator",
	}, []byte(policyDoc), testAuditEvent(policyStoreFixtureTenantID, policyStoreFixtureActorID, audit.EventCreate, audit.ResourcePolicy, policyStoreFixtureSubjectID))
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
		WithArgs(policyStoreFixturePresetID, policyStoreFixtureTenantID).
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow(nil))
	mock.ExpectRollback()

	err = NewPolicyRepository(db).Delete(context.Background(), policyStoreFixtureTenantID, policyStoreFixturePresetID, testAuditEvent(policyStoreFixtureTenantID, policyStoreFixtureActorID, audit.EventDelete, audit.ResourcePolicy, policyStoreFixturePresetID))
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

	err = NewPolicyRepository(db).Delete(context.Background(), policyStoreFixtureTenantID, policyStoreFixtureMissingID, testAuditEvent(policyStoreFixtureTenantID, policyStoreFixtureActorID, audit.EventDelete, audit.ResourcePolicy, policyStoreFixtureMissingID))
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
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow(policyStoreFixtureTenantID))
	mock.ExpectQuery(`SELECT tenant_id FROM policies.*FOR UPDATE`).WithArgs(policyStoreFixtureSubjectID, policyStoreFixtureTenantID).WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow(policyStoreFixtureTenantID))
	mock.ExpectQuery(`SELECT EXISTS .*FROM key_policies`).WithArgs(policyStoreFixtureSubjectID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(`DELETE FROM policies`).
		WithArgs(policyStoreFixtureSubjectID, policyStoreFixtureTenantID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO audit_events`).WithArgs(policyStoreFixtureTenantID, policyStoreFixtureActorID, audit.EventDelete, audit.ResourcePolicy, policyStoreFixtureSubjectID, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := NewPolicyRepository(db).Delete(context.Background(), policyStoreFixtureTenantID, policyStoreFixtureSubjectID, testAuditEvent(policyStoreFixtureTenantID, policyStoreFixtureActorID, audit.EventDelete, audit.ResourcePolicy, policyStoreFixtureSubjectID)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Boundary references remain a foreign-key conflict even with no ordinary
// key_policies binding; they do not use the schema's cascading FK.
func TestPolicyRepository_DeleteRefusesABoundaryReference(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT tenant_id FROM policies`).
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow(policyStoreFixtureTenantID))
	mock.ExpectQuery(`SELECT tenant_id FROM policies.*FOR UPDATE`).WithArgs(policyStoreFixtureSubjectID, policyStoreFixtureTenantID).WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow(policyStoreFixtureTenantID))
	mock.ExpectQuery(`SELECT EXISTS .*FROM key_policies`).WithArgs(policyStoreFixtureSubjectID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(`DELETE FROM policies`).WillReturnError(foreignKeyViolation())
	mock.ExpectRollback()

	err = NewPolicyRepository(db).Delete(context.Background(), policyStoreFixtureTenantID, policyStoreFixtureSubjectID, testAuditEvent(policyStoreFixtureTenantID, policyStoreFixtureActorID, audit.EventDelete, audit.ResourcePolicy, policyStoreFixtureSubjectID))
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
		WithArgs(policyStoreFixtureTenantID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name", "description", "document"}).
			AddRow("preset", nil, "ReadOnlyAccess", "", []byte(policyDoc)).
			AddRow("own", policyStoreFixtureTenantID, "JobOperator", "", []byte(policyDoc)))
	mock.ExpectRollback()

	records, err := NewPolicyRepository(db).List(context.Background(), policyStoreFixtureTenantID)
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
			AddRow("broken", policyStoreFixtureTenantID, "Broken", "", []byte(`not json`)))
	mock.ExpectRollback()

	if _, err := NewPolicyRepository(db).List(context.Background(), policyStoreFixtureTenantID); err == nil {
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
		WithArgs(policyStoreFixtureSubjectID, policyStoreFixtureTenantID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name", "description", "document"}))
	mock.ExpectRollback()

	_, err = NewPolicyRepository(db).Get(context.Background(), policyStoreFixtureTenantID, policyStoreFixtureSubjectID)
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
		WithArgs(policyStoreFixtureSubjectID, policyStoreFixtureTenantID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name", "description", "document"}).
			AddRow(policyStoreFixtureSubjectID, policyStoreFixtureTenantID, "JobOperator", "", []byte(policyDoc)))
	mock.ExpectRollback()

	rec, err := NewPolicyRepository(db).Get(context.Background(), policyStoreFixtureTenantID, policyStoreFixtureSubjectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Name != "JobOperator" {
		t.Fatalf("unexpected record: %+v", rec)
	}
}

var policyStoreFixtureActorID = scan.GenerateID()

var policyStoreFixtureMissingID = scan.GenerateID()

var policyStoreFixtureSubjectID = scan.GenerateID()

var policyStoreFixturePresetID = scan.GenerateID()

var policyStoreFixtureTenantID = scan.GenerateID()

func TestPolicyRepository_DeleteRefusesAnOrdinaryBindingBeforeCascade(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT tenant_id FROM policies`).WithArgs(policyStoreFixtureSubjectID, policyStoreFixtureTenantID).WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow(policyStoreFixtureTenantID))
	mock.ExpectQuery(`SELECT tenant_id FROM policies.*FOR UPDATE`).WithArgs(policyStoreFixtureSubjectID, policyStoreFixtureTenantID).WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow(policyStoreFixtureTenantID))
	mock.ExpectQuery(`SELECT EXISTS .*FROM key_policies`).WithArgs(policyStoreFixtureSubjectID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()
	err = NewPolicyRepository(db).Delete(context.Background(), policyStoreFixtureTenantID, policyStoreFixtureSubjectID, testAuditEvent(policyStoreFixtureTenantID, policyStoreFixtureActorID, audit.EventDelete, audit.ResourcePolicy, policyStoreFixtureSubjectID))
	assertConflict(t, err, "policy", "id")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyRepository_DeleteBindingLookupErrorCannotCascade(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT tenant_id FROM policies`).WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow(policyStoreFixtureTenantID))
	mock.ExpectQuery(`SELECT tenant_id FROM policies.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow(policyStoreFixtureTenantID))
	mock.ExpectQuery(`SELECT EXISTS .*FROM key_policies`).WillReturnError(errors.New("binding read failed"))
	mock.ExpectRollback()
	err = NewPolicyRepository(db).Delete(context.Background(), policyStoreFixtureTenantID, policyStoreFixtureSubjectID, testAuditEvent(policyStoreFixtureTenantID, policyStoreFixtureActorID, audit.EventDelete, audit.ResourcePolicy, policyStoreFixtureSubjectID))
	if err == nil {
		t.Fatal("binding lookup failure allowed deletion")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyRepository_DeleteRechecksOwnerUnderLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT tenant_id FROM policies`).WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow(policyStoreFixtureTenantID))
	mock.ExpectQuery(`SELECT tenant_id FROM policies.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}))
	mock.ExpectRollback()
	err = NewPolicyRepository(db).Delete(context.Background(), policyStoreFixtureTenantID, policyStoreFixtureSubjectID, testAuditEvent(policyStoreFixtureTenantID, policyStoreFixtureActorID, audit.EventDelete, audit.ResourcePolicy, policyStoreFixtureSubjectID))
	assertNotFound(t, err, "policy")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

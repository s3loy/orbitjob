package postgres

import (
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/domain/resource"

	"orbitjob/internal/platform/scan"
)

// sliColumns is the RETURNING/SELECT projection of the slis queries, in the
// order the repositories scan it.
var sliColumns = []string{
	"id", "tenant_id", "name", "description", "sli_type", "source_type",
	"source_config", "aggregation", "good_event_criteria", "version",
	"created_at", "updated_at",
}

const sliDeleteSourceQuery = `SELECT COALESCE(source_config->>'source_uid','') FROM slis WHERE tenant_id=$1 AND id=$2 AND version=$3 AND deleted_at IS NULL AND ($4::text IS NULL OR resource_group_id=$4)`

func newSLIRepoMock(t *testing.T) (*SLIRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewSLIRepository(db), mock
}

func TestSLIRepository_Create(t *testing.T) {
	repo, mock := newSLIRepoMock(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	description := "availability of the checkout API"
	spec := sli.CreateSpec{
		Name:              "checkout-availability",
		Description:       &description,
		SLIType:           "availability",
		SourceType:        "job_run",
		SourceConfig:      map[string]any{"source_uid": "check-7"},
		Aggregation:       "ratio",
		GoodEventCriteria: map[string]any{"status": "success"},
	}

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureSliRepositoryTenantID)
	mock.ExpectExec(`SELECT orbitjob_lock_sli_source\(\$1,\$2\)`).
		WithArgs(coreFixtureSliRepositoryTenantID, "check-7").WillReturnResult(sqlmock.NewResult(0, 0))
	// The insert carries the marshaled JSON documents and a NULL group for an
	// unscoped creator; the RETURNING list is scanned positionally.
	mock.ExpectQuery("INSERT INTO slis").
		WithArgs(
			coreFixtureSliRepositoryTenantID, "checkout-availability", &description, "availability",
			"job_run", []byte(`{"source_uid":"check-7"}`), "ratio",
			[]byte(`{"status":"success"}`), nil,
		).
		WillReturnRows(sqlmock.NewRows(sliColumns).AddRow(
			1, coreFixtureSliRepositoryTenantID, "checkout-availability", &description, "availability",
			"job_run", []byte(`{"source_uid":"check-7"}`), "ratio",
			[]byte(`{"status":"success"}`), 1, now, now,
		))
	expectAuditInsert(mock)
	mock.ExpectCommit()

	snap, err := repo.Create(monitoringActorContext(), coreFixtureSliRepositoryTenantID, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ID != 1 {
		t.Errorf("id = %d, want 1", snap.ID)
	}
	if snap.Name != "checkout-availability" {
		t.Errorf("name = %q, want checkout-availability", snap.Name)
	}
	if snap.SourceConfig["source_uid"] != "check-7" {
		t.Errorf("source_config[source_uid] = %v, want check-7", snap.SourceConfig["source_uid"])
	}
	if snap.GoodEventCriteria["status"] != "success" {
		t.Errorf("good_event_criteria[status] = %v, want success", snap.GoodEventCriteria["status"])
	}
	if snap.Version != 1 {
		t.Errorf("version = %d, want 1", snap.Version)
	}
	if snap.ConfigurationVersion != 1 || snap.StatisticsSemantics != sli.EventTime || !snap.ActiveFrom.Equal(now) {
		t.Fatalf("new SLI lost its configuration boundary: %+v", snap)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLIRepository_Create_BeginTxError(t *testing.T) {
	repo, mock := newSLIRepoMock(t)
	mock.ExpectBegin().WillReturnError(errors.New("boom"))

	if _, err := repo.Create(monitoringActorContext(), coreFixtureSliRepositoryTenantID, sli.CreateSpec{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestSLIRepository_Delete(t *testing.T) {
	repo, mock := newSLIRepoMock(t)
	group := scan.GenerateID()

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureSliRepositoryTenantID)
	mock.ExpectQuery(regexp.QuoteMeta(sliDeleteSourceQuery)).
		WithArgs(coreFixtureSliRepositoryTenantID, int64(1), 2, group).
		WillReturnRows(sqlmock.NewRows([]string{"source_uid"}).AddRow("check-7"))
	mock.ExpectExec(`SELECT orbitjob_lock_sli_source\(\$1,\$2\)`).
		WithArgs(coreFixtureSliRepositoryTenantID, "check-7").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE slis SET deleted_at").
		WithArgs(coreFixtureSliRepositoryTenantID, int64(1), 2, group).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectAuditInsert(mock)
	mock.ExpectCommit()

	if err := repo.Delete(monitoringActorContext(), coreFixtureSliRepositoryTenantID, group, 1, 2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLIRepository_Delete_NotFound(t *testing.T) {
	repo, mock := newSLIRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureSliRepositoryTenantID)
	mock.ExpectQuery(regexp.QuoteMeta(sliDeleteSourceQuery)).
		WithArgs(coreFixtureSliRepositoryTenantID, int64(99), 1, nil).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	err := repo.Delete(monitoringActorContext(), coreFixtureSliRepositoryTenantID, "", 99, 1)
	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *resource.NotFoundError, got %T: %v", err, err)
	}
	if notFound.Resource != "sli" {
		t.Errorf("resource = %q, want sli", notFound.Resource)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLIRepository_Delete_DBError(t *testing.T) {
	repo, mock := newSLIRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureSliRepositoryTenantID)
	mock.ExpectQuery(regexp.QuoteMeta(sliDeleteSourceQuery)).
		WithArgs(coreFixtureSliRepositoryTenantID, int64(1), 2, nil).
		WillReturnRows(sqlmock.NewRows([]string{"source_uid"}).AddRow("check-7"))
	mock.ExpectExec(`SELECT orbitjob_lock_sli_source\(\$1,\$2\)`).
		WithArgs(coreFixtureSliRepositoryTenantID, "check-7").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE slis SET deleted_at").
		WithArgs(coreFixtureSliRepositoryTenantID, int64(1), 2, nil).
		WillReturnError(errors.New("delete failed"))
	mock.ExpectRollback()

	if err := repo.Delete(monitoringActorContext(), coreFixtureSliRepositoryTenantID, "", 1, 2); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLIRepository_FindBySourceUID(t *testing.T) {
	repo, mock := newSLIRepoMock(t)
	now := time.Now()

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureSliRepositoryTenantID)
	mock.ExpectQuery("SELECT (.+) FROM slis WHERE tenant_id = \\$1 AND deleted_at IS NULL").
		WithArgs(coreFixtureSliRepositoryTenantID, "check-42").
		WillReturnRows(sqlmock.NewRows(sliColumns).AddRow(
			3, coreFixtureSliRepositoryTenantID, "checkout-availability", nil, "availability",
			"job_run", []byte(`{"source_uid":"check-42"}`), "ratio",
			[]byte(`{"status":"success"}`), 1, now, now,
		))
	mock.ExpectCommit()

	slis, err := repo.FindBySourceUID(monitoringActorContext(), coreFixtureSliRepositoryTenantID, "check-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(slis) != 1 {
		t.Fatalf("expected 1 sli, got %d", len(slis))
	}
	if slis[0].ID != 3 {
		t.Errorf("id = %d, want 3", slis[0].ID)
	}
	if slis[0].SourceConfig["source_uid"] != "check-42" {
		t.Errorf("source_config[source_uid] = %v, want check-42", slis[0].SourceConfig["source_uid"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLIRepository_FindBySourceUID_DBError(t *testing.T) {
	repo, mock := newSLIRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureSliRepositoryTenantID)
	mock.ExpectQuery("SELECT (.+) FROM slis").
		WithArgs(coreFixtureSliRepositoryTenantID, "check-42").
		WillReturnError(errors.New("query failed"))
	mock.ExpectRollback()

	if _, err := repo.FindBySourceUID(monitoringActorContext(), coreFixtureSliRepositoryTenantID, "check-42"); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

var coreFixtureSliRepositoryTenantID = scan.GenerateID()

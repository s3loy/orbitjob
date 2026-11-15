package postgres

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/sli"

	"orbitjob/internal/platform/scan"
)

// leakRowTime is the timestamp handed to Scan for the RETURNING columns; the
// driver never sees it, sqlmock passes it through unchanged.
var leakRowTime = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

// The SLI repositories used to release their transaction only through
// `defer func() { if err != nil { tx.Rollback() } }()` keyed on the outer err
// variable. Error returns that do not assign that variable -- the not-found
// path in Delete, the json.Unmarshal blocks shadowing err in Create -- came
// back with err == nil and the transaction was never rolled back, pinning a
// pooled connection per call. These tests pin the rollback on exactly those
// paths: an unmet Rollback expectation is the leak.

func TestSLIRepository_Delete_NotFound_RollsBack(t *testing.T) {
	repo, mock := newSLIRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureSliRepositoryLeakTenantID)
	mock.ExpectQuery(regexp.QuoteMeta(sliDeleteSourceQuery)).
		WithArgs(coreFixtureSliRepositoryLeakTenantID, int64(99), 1, nil).
		WillReturnRows(sqlmock.NewRows([]string{"source_uid"}).AddRow("check-7"))
	mock.ExpectExec(`SELECT orbitjob_lock_sli_source\(\$1,\$2\)`).
		WithArgs(coreFixtureSliRepositoryLeakTenantID, "check-7").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE slis SET deleted_at").
		WithArgs(coreFixtureSliRepositoryLeakTenantID, int64(99), 1, nil).
		WillReturnResult(sqlmock.NewResult(0, 0))
	// The zero-rows path returns the not-found error without assigning the
	// outer err, so this rollback only happens when the release does not
	// depend on that variable.
	mock.ExpectRollback()

	if err := repo.Delete(context.Background(), coreFixtureSliRepositoryLeakTenantID, "", 99, 1); err == nil {
		t.Fatal("expected not-found error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("transaction lifecycle incomplete on the not-found path: %v", err)
	}
}

func TestSLIRepository_Create_UnmarshalFailure_RollsBack(t *testing.T) {
	repo, mock := newSLIRepoMock(t)

	mock.ExpectBegin()
	expectSetConfig(mock, coreFixtureSliRepositoryLeakTenantID)
	mock.ExpectExec(`SELECT orbitjob_lock_sli_source\(\$1,\$2\)`).
		WithArgs(coreFixtureSliRepositoryLeakTenantID, "check-7").WillReturnResult(sqlmock.NewResult(0, 0))
	// The insert succeeds and RETURNING hands back a source_config that
	// cannot be unmarshaled; the response-mapping error path must still
	// release the transaction.
	mock.ExpectQuery("INSERT INTO slis").
		WillReturnRows(sqlmock.NewRows(sliColumns).AddRow(
			1, coreFixtureSliRepositoryLeakTenantID, "checkout-availability", nil, "availability",
			"job_run", []byte(`{not json`), "ratio",
			[]byte(`{"status":"success"}`), 1, leakRowTime, leakRowTime,
		))
	mock.ExpectRollback()

	if _, err := repo.Create(context.Background(), coreFixtureSliRepositoryLeakTenantID, sli.CreateSpec{
		Name:         "checkout-availability",
		SLIType:      "availability",
		SourceType:   "job_run",
		SourceConfig: map[string]any{"source_uid": "check-7"},
		Aggregation:  "ratio",
	}); err == nil {
		t.Fatal("expected unmarshal error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("transaction lifecycle incomplete on the unmarshal failure path: %v", err)
	}
}

var coreFixtureSliRepositoryLeakTenantID = scan.GenerateID()

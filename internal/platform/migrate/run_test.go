package migrate

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func testMigrations() []Migration {
	return []Migration{
		{Version: 1, Name: "init", SQL: "SELECT 1", Checksum: strings.Repeat("a", 64)},
		{Version: 2, Name: "more", SQL: "SELECT 2", Checksum: strings.Repeat("b", 64)},
	}
}

func expectLockAndTable(mock sqlmock.Sqlmock) {
	mock.ExpectExec(`SET ROLE orbitjob_table_owner`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`SELECT pg_advisory_lock`).WithArgs(advisoryLockID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS schema_migrations`).WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectUnlock(mock sqlmock.Sqlmock) {
	mock.ExpectExec(`SELECT pg_advisory_unlock`).WithArgs(advisoryLockID).WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestExecuteAppliesPendingMigrations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	expectLockAndTable(mock)
	mock.ExpectQuery(`SELECT version, checksum FROM schema_migrations`).WillReturnRows(sqlmock.NewRows([]string{"version", "checksum"}))
	for _, migration := range testMigrations() {
		mock.ExpectBegin()
		mock.ExpectExec(migration.SQL).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`INSERT INTO schema_migrations`).WithArgs(migration.Version, migration.Name, migration.Checksum).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
	}
	expectUnlock(mock)

	result, err := Execute(context.Background(), db, testMigrations(), Options{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.CurrentVersion != 2 || result.Noop || len(result.Applied) != 2 {
		t.Fatalf("Execute() result = %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteNoop(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	expectLockAndTable(mock)
	migrations := testMigrations()
	mock.ExpectQuery(`SELECT version, checksum FROM schema_migrations`).WillReturnRows(
		sqlmock.NewRows([]string{"version", "checksum"}).
			AddRow(1, migrations[0].Checksum).
			AddRow(2, migrations[1].Checksum),
	)
	expectUnlock(mock)

	result, err := Execute(context.Background(), db, migrations, Options{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Noop || result.CurrentVersion != 2 || len(result.Applied) != 0 {
		t.Fatalf("Execute() result = %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteRejectsChecksumMismatchBeforeApplying(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	expectLockAndTable(mock)
	mock.ExpectQuery(`SELECT version, checksum FROM schema_migrations`).WillReturnRows(
		sqlmock.NewRows([]string{"version", "checksum"}).AddRow(1, strings.Repeat("c", 64)),
	)
	expectUnlock(mock)

	_, err = Execute(context.Background(), db, testMigrations(), Options{})
	if err == nil || !strings.Contains(err.Error(), "migration 0001 checksum mismatch") {
		t.Fatalf("Execute() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteRollsBackFailedMigration(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	expectLockAndTable(mock)
	mock.ExpectQuery(`SELECT version, checksum FROM schema_migrations`).WillReturnRows(sqlmock.NewRows([]string{"version", "checksum"}))
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT 1`).WillReturnError(errors.New("boom"))
	mock.ExpectRollback()
	expectUnlock(mock)

	_, err = Execute(context.Background(), db, testMigrations(), Options{})
	if err == nil || !strings.Contains(err.Error(), "apply migration 0001_init") {
		t.Fatalf("Execute() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteRejectsInvalidBaseline(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	_, err = Execute(context.Background(), db, testMigrations(), Options{BaselineVersion: 3})
	if err == nil || !strings.Contains(err.Error(), "baseline version 3 exceeds latest migration 2") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestRunCompatibilityWrapper(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	expectLockAndTable(mock)
	mock.ExpectQuery(`SELECT version, checksum FROM schema_migrations`).WillReturnRows(sqlmock.NewRows([]string{"version", "checksum"}))
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO schema_migrations`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	expectUnlock(mock)

	migration := testMigrations()[:1]
	if err := Run(context.Background(), db, migration); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

var _ *sql.DB

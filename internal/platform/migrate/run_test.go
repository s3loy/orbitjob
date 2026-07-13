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

func formalBaseline() []Migration {
	return []Migration{{Version: 1, Name: "v020_baseline", SQL: "SELECT 1", Checksum: strings.Repeat("a", 64)}}
}

func expectLock(mock sqlmock.Sqlmock, migrationTable, jobsTable any) {
	mock.ExpectExec(`SET ROLE orbitjob_table_owner`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`SELECT pg_advisory_lock`).WithArgs(advisoryLockID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT\s+to_regclass\('public.schema_migrations'\),\s+to_regclass\('public.jobs'\)`).
		WillReturnRows(sqlmock.NewRows([]string{"migration_table", "jobs_table"}).AddRow(migrationTable, jobsTable))
}

func expectTable(mock sqlmock.Sqlmock) {
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS schema_migrations`).WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectUnlock(mock sqlmock.Sqlmock) {
	mock.ExpectExec(`SELECT pg_advisory_unlock`).WithArgs(advisoryLockID).WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestClassifyExistingHistoryRejectsSchemaWithoutLedger(t *testing.T) {
	err := classifyExistingHistory(formalBaseline(), nil, true)
	if err == nil || err.Error() != UnsupportedPreReleaseHistoryError {
		t.Fatalf("classifyExistingHistory() error = %v", err)
	}
}

func TestClassifyExistingHistoryRejectsOldMigrationName(t *testing.T) {
	err := classifyExistingHistory(formalBaseline(), []AppliedMigration{{Version: 1, Name: "init", Checksum: strings.Repeat("b", 64)}}, true)
	if err == nil || err.Error() != UnsupportedPreReleaseHistoryError {
		t.Fatalf("classifyExistingHistory() error = %v", err)
	}
}

func TestClassifyExistingHistoryRejectsOldVersions(t *testing.T) {
	err := classifyExistingHistory(formalBaseline(), []AppliedMigration{
		{Version: 1, Name: "init", Checksum: strings.Repeat("a", 64)},
		{Version: 2, Name: "scheduler_leaders", Checksum: strings.Repeat("b", 64)},
	}, true)
	if err == nil || err.Error() != UnsupportedPreReleaseHistoryError {
		t.Fatalf("classifyExistingHistory() error = %v", err)
	}
}

func TestClassifyExistingHistoryPreservesFormalChecksumMismatch(t *testing.T) {
	err := classifyExistingHistory(formalBaseline(), []AppliedMigration{{Version: 1, Name: "v020_baseline", Checksum: strings.Repeat("b", 64)}}, true)
	if err == nil || !strings.Contains(err.Error(), "migration 0001 checksum mismatch") {
		t.Fatalf("classifyExistingHistory() error = %v", err)
	}
}

func TestClassifyExistingHistoryAcceptsFormalBaseline(t *testing.T) {
	err := classifyExistingHistory(formalBaseline(), []AppliedMigration{{Version: 1, Name: "v020_baseline", Checksum: strings.Repeat("a", 64)}}, true)
	if err != nil {
		t.Fatalf("classifyExistingHistory() error = %v", err)
	}
}

func TestExecuteAppliesPendingMigrations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	expectLock(mock, nil, nil)
	expectTable(mock)
	mock.ExpectQuery(`SELECT version, name, checksum FROM schema_migrations`).WillReturnRows(sqlmock.NewRows([]string{"version", "name", "checksum"}))
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

	migrations := testMigrations()
	expectLock(mock, "schema_migrations", "jobs")
	expectTable(mock)
	mock.ExpectQuery(`SELECT version, name, checksum FROM schema_migrations`).WillReturnRows(
		sqlmock.NewRows([]string{"version", "name", "checksum"}).
			AddRow(1, migrations[0].Name, migrations[0].Checksum).
			AddRow(2, migrations[1].Name, migrations[1].Checksum),
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

	expectLock(mock, "schema_migrations", "jobs")
	expectTable(mock)
	mock.ExpectQuery(`SELECT version, name, checksum FROM schema_migrations`).WillReturnRows(
		sqlmock.NewRows([]string{"version", "name", "checksum"}).AddRow(1, "init", strings.Repeat("c", 64)),
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

func TestExecuteRejectsPreReleaseSchemaWithoutLedger(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	expectLock(mock, nil, "jobs")
	expectUnlock(mock)

	_, err = Execute(context.Background(), db, formalBaseline(), Options{})
	if err == nil || err.Error() != UnsupportedPreReleaseHistoryError {
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

	expectLock(mock, nil, nil)
	expectTable(mock)
	mock.ExpectQuery(`SELECT version, name, checksum FROM schema_migrations`).WillReturnRows(sqlmock.NewRows([]string{"version", "name", "checksum"}))
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

func TestRunCompatibilityWrapper(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	expectLock(mock, nil, nil)
	expectTable(mock)
	mock.ExpectQuery(`SELECT version, name, checksum FROM schema_migrations`).WillReturnRows(sqlmock.NewRows([]string{"version", "name", "checksum"}))
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO schema_migrations`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	expectUnlock(mock)

	if err := Run(context.Background(), db, testMigrations()[:1]); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

var _ *sql.DB

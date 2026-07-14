package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	advisoryLockID                    int64 = 0x4f524249544a4f42 // ORBITJOB
	UnsupportedPreReleaseHistoryError       = "unsupported pre-release schema history; recreate the database for v0.2.0"
)

type Logger interface {
	Printf(format string, args ...any)
}

type Result struct {
	CurrentVersion int
	Applied        []int
	Noop           bool
}

type Options struct {
	Logger Logger
}

type AppliedMigration struct {
	Version  int
	Name     string
	Checksum string
}

func Run(ctx context.Context, db *sql.DB, migrations []Migration) error {
	_, err := Execute(ctx, db, migrations, Options{})
	return err
}

func RunWithOptions(ctx context.Context, db *sql.DB, migrations []Migration, options Options) error {
	_, err := Execute(ctx, db, migrations, options)
	return err
}

func Execute(ctx context.Context, db *sql.DB, migrations []Migration, options Options) (Result, error) {
	if len(migrations) == 0 {
		return Result{}, fmt.Errorf("at least one migration is required")
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("open migration connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, `SET ROLE orbitjob_table_owner`); err != nil {
		return Result{}, fmt.Errorf("assume migration owner role: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockID); err != nil {
		return Result{}, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock($1)`, advisoryLockID)
	}()

	var migrationTable sql.NullString
	var jobsTable sql.NullString
	if err := conn.QueryRowContext(ctx, `
		SELECT
			to_regclass('public.schema_migrations'),
			to_regclass('public.jobs')
	`).Scan(&migrationTable, &jobsTable); err != nil {
		return Result{}, fmt.Errorf("inspect existing schema: %w", err)
	}
	if isV020BaselineSet(migrations) && !migrationTable.Valid && jobsTable.Valid {
		return Result{}, errors.New(UnsupportedPreReleaseHistoryError)
	}

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			checksum CHAR(64) NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return Result{}, fmt.Errorf("create schema_migrations: %w", err)
	}

	rows, err := conn.QueryContext(ctx, `SELECT version, name, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return Result{}, fmt.Errorf("read migration state: %w", err)
	}
	applied := make([]AppliedMigration, 0)
	appliedByVersion := make(map[int]AppliedMigration)
	currentVersion := 0
	for rows.Next() {
		var migration AppliedMigration
		if err := rows.Scan(&migration.Version, &migration.Name, &migration.Checksum); err != nil {
			_ = rows.Close()
			return Result{}, fmt.Errorf("scan migration state: %w", err)
		}
		applied = append(applied, migration)
		appliedByVersion[migration.Version] = migration
		if migration.Version > currentVersion {
			currentVersion = migration.Version
		}
	}
	if err := rows.Close(); err != nil {
		return Result{}, fmt.Errorf("close migration state: %w", err)
	}
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("iterate migration state: %w", err)
	}
	if err := classifyExistingHistory(migrations, applied, jobsTable.Valid); err != nil {
		return Result{}, err
	}

	result := Result{CurrentVersion: currentVersion}
	for _, migration := range migrations {
		if _, ok := appliedByVersion[migration.Version]; ok {
			continue
		}
		started := time.Now()
		logf(options.Logger, "applying migration %04d_%s", migration.Version, migration.Name)
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return Result{}, fmt.Errorf("begin migration %04d: %w", migration.Version, err)
		}
		if _, err = tx.ExecContext(ctx, migration.SQL); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, checksum) VALUES ($1, $2, $3)`, migration.Version, migration.Name, migration.Checksum)
		}
		if err != nil {
			_ = tx.Rollback()
			return Result{}, fmt.Errorf("apply migration %04d_%s: %w", migration.Version, migration.Name, err)
		}
		if err := tx.Commit(); err != nil {
			return Result{}, fmt.Errorf("commit migration %04d: %w", migration.Version, err)
		}
		result.Applied = append(result.Applied, migration.Version)
		result.CurrentVersion = migration.Version
		logf(options.Logger, "applied migration %04d_%s in %s", migration.Version, migration.Name, time.Since(started))
	}
	result.Noop = len(result.Applied) == 0
	if result.Noop {
		logf(options.Logger, "schema is current at version %04d", result.CurrentVersion)
	}
	return result, nil
}

func isV020BaselineSet(migrations []Migration) bool {
	return len(migrations) == 1 && migrations[0].Version == 1 && migrations[0].Name == "v020_baseline"
}

func classifyExistingHistory(migrations []Migration, applied []AppliedMigration, jobsExists bool) error {
	if !isV020BaselineSet(migrations) {
		return validateChecksums(migrations, applied)
	}
	if len(applied) == 0 {
		if jobsExists {
			return errors.New(UnsupportedPreReleaseHistoryError)
		}
		return nil
	}
	if len(applied) != 1 || applied[0].Version != 1 || applied[0].Name != "v020_baseline" {
		return errors.New(UnsupportedPreReleaseHistoryError)
	}
	if applied[0].Checksum != migrations[0].Checksum {
		return fmt.Errorf("migration 0001 checksum mismatch")
	}
	return nil
}

func validateChecksums(migrations []Migration, applied []AppliedMigration) error {
	known := make(map[int]Migration, len(migrations))
	for _, migration := range migrations {
		known[migration.Version] = migration
	}
	for _, existing := range applied {
		migration, ok := known[existing.Version]
		if !ok {
			continue
		}
		if existing.Checksum != migration.Checksum {
			return fmt.Errorf("migration %04d checksum mismatch", existing.Version)
		}
	}
	return nil
}

func logf(logger Logger, format string, args ...any) {
	if logger != nil {
		logger.Printf(format, args...)
	}
}

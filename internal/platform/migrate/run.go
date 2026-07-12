package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const advisoryLockID int64 = 0x4f524249544a4f42 // ORBITJOB

type Logger interface {
	Printf(format string, args ...any)
}

type Result struct {
	CurrentVersion int
	Applied        []int
	Noop           bool
}

type Options struct {
	BaselineVersion int
	Logger          Logger
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
	latestVersion := migrations[len(migrations)-1].Version
	if options.BaselineVersion < 0 {
		return Result{}, fmt.Errorf("baseline version must be non-negative")
	}
	if options.BaselineVersion > latestVersion {
		return Result{}, fmt.Errorf("baseline version %d exceeds latest migration %d", options.BaselineVersion, latestVersion)
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

	rows, err := conn.QueryContext(ctx, `SELECT version, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return Result{}, fmt.Errorf("read migration state: %w", err)
	}
	appliedChecksums := make(map[int]string)
	currentVersion := 0
	for rows.Next() {
		var version int
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			_ = rows.Close()
			return Result{}, fmt.Errorf("scan migration state: %w", err)
		}
		appliedChecksums[version] = checksum
		if version > currentVersion {
			currentVersion = version
		}
	}
	if err := rows.Close(); err != nil {
		return Result{}, fmt.Errorf("close migration state: %w", err)
	}
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("iterate migration state: %w", err)
	}

	for _, migration := range migrations {
		if checksum, ok := appliedChecksums[migration.Version]; ok && checksum != migration.Checksum {
			return Result{}, fmt.Errorf("migration %04d checksum mismatch", migration.Version)
		}
	}

	result := Result{CurrentVersion: currentVersion}
	for _, migration := range migrations {
		if _, ok := appliedChecksums[migration.Version]; ok {
			continue
		}
		if migration.Version <= options.BaselineVersion {
			if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, checksum) VALUES ($1, $2, $3)`, migration.Version, migration.Name, migration.Checksum); err != nil {
				return Result{}, fmt.Errorf("baseline migration %04d: %w", migration.Version, err)
			}
			result.Applied = append(result.Applied, migration.Version)
			result.CurrentVersion = migration.Version
			logf(options.Logger, "baselined migration %04d_%s", migration.Version, migration.Name)
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

func logf(logger Logger, format string, args ...any) {
	if logger != nil {
		logger.Printf(format, args...)
	}
}

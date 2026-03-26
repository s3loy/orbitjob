package postgrestest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"orbitjob/internal/platform/config"
)

const (
	sharedSchemaLockClassID   = 32117
	sharedSchemaLockObjectID  = 260326
	sharedPackageLockClassID  = 32117
	sharedPackageLockObjectID = 260327
	advisoryLockWaitTimeout   = 5 * time.Minute
)

// Run prepares the integration database before package tests execute.
func Run(m *testing.M) int {
	if err := config.LoadDotenv(); err != nil {
		fmt.Fprintf(os.Stderr, "load .env: %v\n", err)
		return 1
	}

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		return m.Run()
	}

	exitCode := 0
	if err := withAdvisoryLock(
		dsn,
		sharedPackageLockClassID,
		sharedPackageLockObjectID,
		func(db *sql.DB) error {
			if err := applySchemaWithDB(dsn, db); err != nil {
				return err
			}

			exitCode = m.Run()
			return nil
		},
	); err != nil {
		fmt.Fprintf(os.Stderr, "apply test schema: %v\n", err)
		return 1
	}

	return exitCode
}

// DSN returns the test database DSN or skips the integration test package.
func DSN(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	return dsn
}

// Open returns a real PostgreSQL handle with test data reset for the caller.
func Open(t *testing.T) *sql.DB {
	t.Helper()

	db, err := open(DSN(t))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping test db: %v", err)
	}
	if err := resetTestData(ctx, db); err != nil {
		t.Fatalf("reset test data: %v", err)
	}

	return db
}

func open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	return db, nil
}

func resetTestData(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		TRUNCATE TABLE
			job_change_audits,
			job_instance_attempts,
			job_instances,
			workers,
			jobs
		RESTART IDENTITY CASCADE
	`)
	return err
}

func applySchemaWithDB(dsn string, db *sql.DB) error {
	if err := validateDSN(dsn); err != nil {
		return err
	}

	path, err := findMigrationFile("db", "migrations", "0001_init.sql")
	if err != nil {
		return err
	}

	sqlBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, string(sqlBytes)); err != nil {
		return err
	}

	return resetTestData(ctx, db)
}

// withAdvisoryLock serializes shared test-database access across go test processes.
func withAdvisoryLock(dsn string, classID, objectID int, fn func(db *sql.DB) error) (err error) {
	if err := validateDSN(dsn); err != nil {
		return err
	}

	db, err := open(dsn)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), advisoryLockWaitTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return err
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := conn.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1, $2)`, classID, objectID); err != nil {
		return err
	}

	locked := true
	defer func() {
		if !locked {
			return
		}

		unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer unlockCancel()
		if _, unlockErr := conn.ExecContext(
			unlockCtx,
			`SELECT pg_advisory_unlock($1, $2)`,
			classID,
			objectID,
		); err == nil && unlockErr != nil {
			err = unlockErr
		}
	}()

	err = fn(db)
	if err != nil {
		return err
	}

	locked = false
	unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer unlockCancel()
	if _, err := conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock($1, $2)`, classID, objectID); err != nil {
		return err
	}

	return nil
}

func validateDSN(dsn string) error {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return err
	}

	dbName := strings.TrimPrefix(parsed.Path, "/")
	if dbName == "" {
		return fmt.Errorf("test database name is required")
	}
	if strings.Contains(strings.ToLower(dbName), "test") {
		return nil
	}

	return fmt.Errorf("TEST_DATABASE_DSN must point to a dedicated test database, got %q", dbName)
}

func findMigrationFile(parts ...string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := wd
	for {
		path := filepath.Join(append([]string{dir}, parts...)...)
		info, err := os.Stat(path)
		switch {
		case err == nil && !info.IsDir():
			return path, nil
		case err != nil && !os.IsNotExist(err):
			return "", err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

package postgrestest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"orbitjob/internal/platform/config"
)

const (
	sharedSchemaLockClassID  = 32117
	sharedSchemaLockObjectID = 260326
	advisoryLockWaitTimeout  = 5 * time.Minute
	testSchemaPrefix         = "ojtest_"
	maxIdentifierLength      = 63
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

	packageDSN, _, err := packageDSN(dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scope test dsn: %v\n", err)
		return 1
	}
	if err := os.Setenv("TEST_DATABASE_DSN", packageDSN); err != nil {
		fmt.Fprintf(os.Stderr, "set TEST_DATABASE_DSN: %v\n", err)
		return 1
	}

	if err := withAdvisoryLock(
		packageDSN,
		sharedSchemaLockClassID,
		sharedSchemaLockObjectID,
		func(db *sql.DB) error {
			return applySchemaWithDB(packageDSN, db)
		},
	); err != nil {
		fmt.Fprintf(os.Stderr, "apply test schema: %v\n", err)
		return 1
	}

	return m.Run()
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

// Open returns a PostgreSQL handle scoped to the current test name.
func Open(t *testing.T) *sql.DB {
	t.Helper()

	dsn, schemaName, err := testDSN(DSN(t), t.Name())
	if err != nil {
		t.Fatalf("scope test dsn: %v", err)
	}

	db, err := open(dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping test db: %v", err)
	}
	if err := withAdvisoryLock(
		dsn,
		sharedSchemaLockClassID,
		lockObjectID(dsn),
		func(_ *sql.DB) error {
			return applySchemaWithDB(dsn, db)
		},
	); err != nil {
		t.Fatalf("prepare test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()

		if err := dropSchema(cleanupCtx, db, schemaName); err != nil {
			t.Errorf("drop test schema %q: %v", schemaName, err)
		}
	})

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
			audit_events,
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
	schemaName, err := schemaNameFromDSN(dsn)
	if err != nil {
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

	if err := ensureSchema(ctx, db, schemaName); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, string(sqlBytes)); err != nil {
		return err
	}

	return resetTestData(ctx, db)
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

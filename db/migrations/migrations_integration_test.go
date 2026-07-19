//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"orbitjob/internal/platform/config"
	platformmigrate "orbitjob/internal/platform/migrate"
)

var testPasswords = map[string]string{
	"orbitjob_migrator": "v020-test-migrator",
	"orbitjob_admin":    "v020-test-admin",
	"orbitjob_runtime":  "v020-test-runtime",
	"orbitjob_operator": "v020-test-operator",
}

func testOwnerDSN(t *testing.T) string {
	t.Helper()
	if err := config.LoadDotenv(); err != nil {
		t.Fatalf("load .env: %v", err)
	}
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	return dsn
}

func testRoleDSN(t *testing.T, role string) string {
	t.Helper()
	parsed, err := url.Parse(testOwnerDSN(t))
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_DSN: %v", err)
	}
	parsed.User = url.UserPassword(role, testPasswords[role])
	return parsed.String()
}

func openMigrationDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func prepareMigrationRoles(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := openMigrationDB(t, testOwnerDSN(t))
	if err := platformmigrate.EnsureRoles(ctx, db); err != nil {
		t.Fatalf("EnsureRoles() error = %v", err)
	}
	if err := platformmigrate.EnsureRolePasswords(ctx, db, testPasswords); err != nil {
		t.Fatalf("EnsureRolePasswords() error = %v", err)
	}
}

func resetPublicSchema(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := openMigrationDB(t, testOwnerDSN(t))
	if _, err := db.ExecContext(ctx, `
		DROP SCHEMA public CASCADE;
		CREATE SCHEMA public;
		GRANT ALL ON SCHEMA public TO PUBLIC;
	`); err != nil {
		t.Fatalf("reset public schema: %v", err)
	}
}

func applyV020Baseline(t *testing.T) {
	t.Helper()
	prepareMigrationRoles(t)
	resetPublicSchema(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	migrations, err := platformmigrate.Load(os.DirFS("."), ".")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	migrator := openMigrationDB(t, testRoleDSN(t, "orbitjob_migrator"))
	result, err := platformmigrate.Execute(ctx, migrator, migrations, platformmigrate.Options{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.CurrentVersion != 3 || result.Noop || len(result.Applied) != 3 {
		t.Fatalf("Execute() result = %#v", result)
	}
}

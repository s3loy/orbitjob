//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"orbitjob/internal/platform/config"
	platformmigrate "orbitjob/internal/platform/migrate"
)

var testPasswords = map[string]string{
	"orbitjob_migrator": "itest-migrator",
	"orbitjob_admin":    "itest-admin",
	"orbitjob_runtime":  "itest-runtime",
	"orbitjob_operator": "itest-operator",
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

func applyAllMigrations(t *testing.T) {
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
	expectedVersion := migrations[len(migrations)-1].Version
	if result.CurrentVersion != expectedVersion || result.Noop || len(result.Applied) != len(migrations) {
		t.Fatalf("Execute() result = %#v", result)
	}
}

// ownerDB opens the owner (superuser) connection. RLS does not apply to it, so
// it is how a test seeds rows for more than one tenant; the isolation is then
// observed through a non-owner connection.
func ownerDB(t *testing.T) *sql.DB {
	t.Helper()
	return openMigrationDB(t, testOwnerDSN(t))
}

// withRoleConn runs fn on a single connection opened as role. A *sql.DB pools
// connections, so a session setting such as SET app.tenant_id issued on the
// pool may land on one connection and be read from another; every statement in
// one tenant context must share a connection.
func withRoleConn(t *testing.T, role string, fn func(ctx context.Context, conn *sql.Conn)) {
	t.Helper()
	db := openMigrationDB(t, testRoleDSN(t, role))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open connection as %s: %v", role, err)
	}
	defer func() { _ = conn.Close() }()
	fn(ctx, conn)
}

// names runs a query expected to return one text column and returns every value.
func names(t *testing.T, ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, query string) []string {
	t.Helper()
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	return out
}

// baselineStructuralAssertion returns the in-file catalog assertion block. The
// tests that probe the assertion run this exact text rather than a copy, so a
// weakened assertion fails the probe.
func baselineStructuralAssertion(t *testing.T) string {
	t.Helper()
	const begin = "-- BEGIN structural catalog assertion"
	const end = "-- END structural catalog assertion"
	text := string(readBaseline(t))
	from := strings.Index(text, begin)
	to := strings.Index(text, end)
	if from < 0 || to < 0 || to < from {
		t.Fatalf("baseline is missing the structural catalog assertion block")
	}
	return text[from : to+len(end)]
}

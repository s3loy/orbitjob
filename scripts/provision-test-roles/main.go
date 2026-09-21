// Command provision-test-roles prepares a fresh PostgreSQL instance for the
// integration suites: it creates the hardened orbitjob_* deployment roles and
// sets their login passwords to the itest-* values the suites authenticate
// with. CI runs it against the service's superuser DSN before any suite
// executes, so a role-provisioning failure surfaces as this step failing
// rather than as confusing in-suite errors. The suites repeat the same calls
// on their own; against provisioned roles both are cheap no-ops.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/lib/pq"

	"orbitjob/internal/platform/migrate"
)

// These must stay identical to the passwords baked into the integration
// suites (db/migrations, internal/admin/bootstrap, internal/admin/http);
// they are test-only values, never deployment secrets.
var passwords = map[string]string{
	migrate.RoleMigrator: "itest-migrator",
	migrate.RoleAdmin:    "itest-admin",
	migrate.RoleRuntime:  "itest-runtime",
}

const connectTimeout = 60 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "provision-test-roles:", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		return fmt.Errorf("TEST_DATABASE_DSN is required")
	}

	db, err := connectWithRetry(dsn, connectTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	if err := migrate.EnsureRoles(ctx, db); err != nil {
		return fmt.Errorf("ensure roles: %w", err)
	}
	if err := migrate.EnsureRolePasswords(ctx, db, passwords); err != nil {
		return fmt.Errorf("ensure role passwords: %w", err)
	}
	fmt.Println("deployment roles provisioned")
	return nil
}

// connectWithRetry retries until the deadline: a freshly started service
// container reports healthy before it accepts the first connection.
func connectWithRetry(dsn string, timeout time.Duration) (*sql.DB, error) {
	deadline := time.Now().Add(timeout)
	for attempt := 1; ; attempt++ {
		db, err := sql.Open("postgres", dsn)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err = db.PingContext(ctx)
			cancel()
			if err == nil {
				return db, nil
			}
			_ = db.Close()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("database not reachable after %s: %w", timeout, err)
		}
		if attempt%5 == 1 {
			fmt.Printf("database not ready (attempt %d): %v\n", attempt, err)
		}
		time.Sleep(2 * time.Second)
	}
}

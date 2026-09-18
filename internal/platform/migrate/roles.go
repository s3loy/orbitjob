package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/lib/pq"
)

const (
	RoleMigrator = "orbitjob_migrator"
	RoleAdmin    = "orbitjob_admin"
	RoleRuntime  = "orbitjob_runtime"
)

var managedLoginRoles = []string{RoleMigrator, RoleAdmin, RoleRuntime}

// querier is the read/execute surface EnsureRolePasswords' helpers need. Both
// *sql.DB and *sql.Tx satisfy it, so the password verification and the ALTER
// can run inside the same locking transaction.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// ManagedLoginRoles returns the fixed deployment login roles.
func ManagedLoginRoles() []string {
	return append([]string(nil), managedLoginRoles...)
}

// RoleSetupLockKey is the two-part advisory lock EnsureRoles holds while
// creating extensions and roles. The values mirror postgrestest's
// sharedSchemaLockClassID/extensionLockObjectID: postgrestest imports this
// package, so the mirror cannot go the other way, and a drift test there
// fails if either copy changes. Holding the same two-key lock as the
// harness's extension placement is what serializes the two CREATE EXTENSION
// pgcrypto calls against each other — IF NOT EXISTS does not, because its
// check and insert are not atomic across sessions.
const (
	RoleSetupLockClassID  = 32117
	RoleSetupLockObjectID = 260327
)

// EnsureRoles creates and hardens the deployment login roles before numbered
// migrations run with the restricted migrator identity.
func EnsureRoles(ctx context.Context, db *sql.DB) error {
	// Session-scoped lock on a dedicated connection: database/sql pools
	// ExecContext across connections, and an advisory lock must be taken and
	// released on the same session.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for role setup: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx,
		`SELECT pg_advisory_lock($1, $2)`, RoleSetupLockClassID, RoleSetupLockObjectID); err != nil {
		return fmt.Errorf("acquire role setup lock: %w", err)
	}
	locked := true
	defer func() {
		if locked {
			_, _ = conn.ExecContext(ctx,
				`SELECT pg_advisory_unlock($1, $2)`, RoleSetupLockClassID, RoleSetupLockObjectID)
		}
	}()
	if _, err := conn.ExecContext(ctx,
		`CREATE EXTENSION IF NOT EXISTS pgcrypto SCHEMA public`); err != nil {
		return fmt.Errorf("ensure pgcrypto extension: %w", err)
	}
	const statement = `
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orbitjob_table_owner') THEN
    CREATE ROLE orbitjob_table_owner NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orbitjob_migrator') THEN
    CREATE ROLE orbitjob_migrator LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orbitjob_admin') THEN
    CREATE ROLE orbitjob_admin LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orbitjob_runtime') THEN
    CREATE ROLE orbitjob_runtime LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orbitjob_operator') THEN
    CREATE ROLE orbitjob_operator NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orbitjob_owner') THEN
    CREATE ROLE orbitjob_owner NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orbitjob_reader') THEN
    CREATE ROLE orbitjob_reader NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
  END IF;
END $$;

-- Verify the roles are hardened rather than re-asserting it.
--
-- These were unconditional ALTER ROLE statements. PostgreSQL lets only a
-- superuser change SUPERUSER, CREATEROLE or BYPASSRLS, so running them meant
-- owner-init required a superuser connection -- which is why the installation
-- Secret carried a superuser DSN. The attributes are already correct on every
-- role this function creates, so the ALTER only ever mattered for a role that
-- had drifted, and that case needs a superuser regardless. Checking reports the
-- drift instead of depending on the privilege to silently repair it.
DO $$
DECLARE
  expected record;
  actual record;
BEGIN
  FOR expected IN
    SELECT * FROM (VALUES
      ('orbitjob_table_owner', false),
      ('orbitjob_migrator',    true),
      ('orbitjob_admin',       true),
      ('orbitjob_runtime',     true),
      ('orbitjob_operator',    false),
      ('orbitjob_owner',       false),
      ('orbitjob_reader',      false)
    ) AS t(role_name, can_login)
  LOOP
    SELECT rolsuper, rolbypassrls, rolcreaterole, rolinherit, rolcanlogin
      INTO actual
      FROM pg_roles WHERE rolname = expected.role_name;
    CONTINUE WHEN NOT FOUND;

    IF actual.rolsuper OR actual.rolbypassrls OR actual.rolcreaterole OR actual.rolinherit THEN
      RAISE EXCEPTION
        'role % is not hardened: rolsuper=%, rolbypassrls=%, rolcreaterole=%, rolinherit=%. A superuser has to repair it before owner-init can continue',
        expected.role_name, actual.rolsuper, actual.rolbypassrls, actual.rolcreaterole, actual.rolinherit;
    END IF;
    IF actual.rolcanlogin <> expected.can_login THEN
      RAISE EXCEPTION
        'role % has LOGIN=%, expected %. A superuser has to repair it before owner-init can continue',
        expected.role_name, actual.rolcanlogin, expected.can_login;
    END IF;
  END LOOP;
END $$;

DO $$
DECLARE
  membership record;
BEGIN
  FOR membership IN
    SELECT member_role.rolname AS member_name, parent_role.rolname AS parent_name
    FROM pg_auth_members am
    JOIN pg_roles member_role ON member_role.oid = am.member
    JOIN pg_roles parent_role ON parent_role.oid = am.roleid
    WHERE member_role.rolname IN ('orbitjob_migrator', 'orbitjob_admin', 'orbitjob_runtime', 'orbitjob_operator')
      AND NOT (member_role.rolname = 'orbitjob_migrator' AND parent_role.rolname = 'orbitjob_table_owner')
  LOOP
    EXECUTE format('REVOKE %I FROM %I', membership.parent_name, membership.member_name);
  END LOOP;
END $$;
GRANT orbitjob_table_owner TO orbitjob_migrator;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA public TO orbitjob_table_owner;
`
	if _, err := conn.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("ensure database roles: %w", err)
	}
	locked = false
	if _, err := conn.ExecContext(ctx,
		`SELECT pg_advisory_unlock($1, $2)`, RoleSetupLockClassID, RoleSetupLockObjectID); err != nil {
		return fmt.Errorf("release role setup lock: %w", err)
	}
	return nil
}

// passwordAlreadyCorrect verifies whether the given password is already set for
// the role by opening a short-lived test connection. It returns true when the
// connection succeeds, false when authentication fails, and an error only for
// unexpected failures. The handle is *sql.DB or *sql.Tx — the verification
// itself always dials a fresh connection, so it works from inside the locking
// transaction without touching that transaction's session.
var passwordAlreadyCorrect = func(ctx context.Context, db querier, role, password string) (bool, error) {
	var host, port, dbname string
	row := db.QueryRowContext(ctx, "SELECT inet_server_addr()::text, inet_server_port()::text, current_database()")
	if err := row.Scan(&host, &port, &dbname); err != nil {
		return false, fmt.Errorf("read connection info: %w", err)
	}
	// Build a URL-style DSN so that special characters in the password are
	// correctly percent-encoded.
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(role, password),
		Host:     net.JoinHostPort(host, port),
		Path:     "/" + dbname,
		RawQuery: "sslmode=prefer&connect_timeout=3",
	}
	testDB, err := sql.Open("postgres", u.String())
	if err != nil {
		return false, fmt.Errorf("open test connection for %s: %w", role, err)
	}
	defer func() { _ = testDB.Close() }()
	testCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := testDB.PingContext(testCtx); err != nil {
		// Authentication failure means the password doesn't match — not an error.
		if strings.Contains(err.Error(), "password authentication failed") ||
			strings.Contains(err.Error(), "invalid password") {
			return false, nil
		}
		// Other errors (network, timeout) are treated as "cannot verify" —
		// fall through to ALTER ROLE as a safe default.
		return false, nil
	}
	return true, nil
}

// EnsureRolePasswords sets deployment-generated passwords after role-creation
// migrations have completed. Passwords are never stored in migration files.
// On repeated runs it verifies each password before issuing ALTER ROLE so that
// an upgrade never overwrites passwords that running pods still depend on.
func EnsureRolePasswords(ctx context.Context, db *sql.DB, passwords map[string]string) error {
	for _, role := range managedLoginRoles {
		if passwords[role] == "" {
			return fmt.Errorf("password for %s is required", role)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin role password transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Hold the role-setup lock for verify-and-mutate as one unit — the same
	// two-key lock EnsureRoles holds, because CREATE ROLE and ALTER ROLE
	// mutate the same catalog rows and "tuple concurrently updated" is what
	// their overlap produces. Releasing between the check and the ALTER would
	// let two waiters interleave again. Exec (not Query) is deliberate: lib/pq
	// forbids a QueryRow returning no rows on a statement that yields no result
	// set, and swallowing the row via ExecContext is the documented way to run
	// a value-returning SELECT for effect only.
	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock($1, $2)`, RoleSetupLockClassID, RoleSetupLockObjectID); err != nil {
		return fmt.Errorf("acquire role password lock: %w", err)
	}
	for _, role := range managedLoginRoles {
		correct, err := passwordAlreadyCorrect(ctx, tx, role, passwords[role])
		if err != nil {
			return fmt.Errorf("verify password for %s: %w", role, err)
		}
		if correct {
			continue
		}
		statement := fmt.Sprintf("ALTER ROLE %s PASSWORD %s", pq.QuoteIdentifier(role), pq.QuoteLiteral(passwords[role]))
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("set password for %s: %w", role, err)
		}
	}
	return tx.Commit()
}

package migrate

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lib/pq"
)

const (
	RoleMigrator = "orbitjob_migrator"
	RoleAdmin    = "orbitjob_admin"
	RoleRuntime  = "orbitjob_runtime"
)

var managedLoginRoles = []string{RoleMigrator, RoleAdmin, RoleRuntime}

// ManagedLoginRoles returns the fixed deployment login roles.
func ManagedLoginRoles() []string {
	return append([]string(nil), managedLoginRoles...)
}

// EnsureRoles creates and hardens the deployment login roles before numbered
// migrations run with the restricted migrator identity.
func EnsureRoles(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
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
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orbitjob_dispatcher') THEN
    CREATE ROLE orbitjob_dispatcher NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orbitjob_worker') THEN
    CREATE ROLE orbitjob_worker NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orbitjob_reader') THEN
    CREATE ROLE orbitjob_reader NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
  END IF;
END $$;
ALTER ROLE orbitjob_table_owner NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
ALTER ROLE orbitjob_migrator LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
ALTER ROLE orbitjob_admin LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
ALTER ROLE orbitjob_runtime LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
ALTER ROLE orbitjob_operator NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
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
	if _, err := db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("ensure database roles: %w", err)
	}
	return nil
}

// EnsureRolePasswords sets deployment-generated passwords after role-creation
// migrations have completed. Passwords are never stored in migration files.
func EnsureRolePasswords(ctx context.Context, db *sql.DB, passwords map[string]string) error {
	for _, role := range managedLoginRoles {
		if passwords[role] == "" {
			return fmt.Errorf("password for %s is required", role)
		}
	}
	for _, role := range managedLoginRoles {
		statement := fmt.Sprintf("ALTER ROLE %s PASSWORD %s", pq.QuoteIdentifier(role), pq.QuoteLiteral(passwords[role]))
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("set password for %s: %w", role, err)
		}
	}
	return nil
}

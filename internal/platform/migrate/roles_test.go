package migrate

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestManagedLoginRolesReturnsCopy(t *testing.T) {
	roles := ManagedLoginRoles()
	if len(roles) != 3 {
		t.Fatalf("ManagedLoginRoles() length = %d, want 3", len(roles))
	}
	roles[0] = "changed"
	if managedLoginRoles[0] != RoleMigrator {
		t.Fatal("ManagedLoginRoles() exposed internal slice")
	}
}

func TestEnsureRolesExecutesIdempotentRoleSetup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`DO \$\$`).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := EnsureRoles(context.Background(), db); err != nil {
		t.Fatalf("EnsureRoles() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRolePasswordsRequiresEveryPasswordBeforeWriting(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	err = EnsureRolePasswords(context.Background(), db, map[string]string{
		RoleMigrator: "migrator",
		RoleAdmin:    "admin",
	})
	if err == nil || !strings.Contains(err.Error(), "password for orbitjob_runtime is required") {
		t.Fatalf("EnsureRolePasswords() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRolePasswordsUsesFixedAllowlist(t *testing.T) {
	orig := passwordAlreadyCorrect
	passwordAlreadyCorrect = func(_ context.Context, _ *sql.DB, _, _ string) (bool, error) { return false, nil }
	defer func() { passwordAlreadyCorrect = orig }()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	passwords := map[string]string{
		RoleMigrator: "migrator",
		RoleAdmin:    "admin",
		RoleRuntime:  "runtime",
		"attacker":   "ignored",
	}
	for _, role := range managedLoginRoles {
		mock.ExpectExec("ALTER ROLE \\\"" + role + "\\\" PASSWORD '").WillReturnResult(sqlmock.NewResult(0, 0))
	}

	if err := EnsureRolePasswords(context.Background(), db, passwords); err != nil {
		t.Fatalf("EnsureRolePasswords() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRolePasswordsSkipsWhenPasswordAlreadyCorrect(t *testing.T) {
	orig := passwordAlreadyCorrect
	passwordAlreadyCorrect = func(_ context.Context, _ *sql.DB, _, _ string) (bool, error) { return true, nil }
	defer func() { passwordAlreadyCorrect = orig }()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	passwords := map[string]string{
		RoleMigrator: "migrator",
		RoleAdmin:    "admin",
		RoleRuntime:  "runtime",
	}
	// All three passwords are already correct — zero ALTER ROLE calls expected.

	if err := EnsureRolePasswords(context.Background(), db, passwords); err != nil {
		t.Fatalf("EnsureRolePasswords() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRolePasswordsPropagatesVerificationError(t *testing.T) {
	orig := passwordAlreadyCorrect
	passwordAlreadyCorrect = func(_ context.Context, _ *sql.DB, _, _ string) (bool, error) {
		return false, errors.New("cannot reach server")
	}
	defer func() { passwordAlreadyCorrect = orig }()

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	passwords := map[string]string{
		RoleMigrator: "migrator",
		RoleAdmin:    "admin",
		RoleRuntime:  "runtime",
	}

	err = EnsureRolePasswords(context.Background(), db, passwords)
	if err == nil || !strings.Contains(err.Error(), "verify password for orbitjob_migrator") {
		t.Fatalf("EnsureRolePasswords() error = %v", err)
	}
}

func TestEnsureRolePasswordsPropagatesDatabaseError(t *testing.T) {
	orig := passwordAlreadyCorrect
	passwordAlreadyCorrect = func(_ context.Context, _ *sql.DB, _, _ string) (bool, error) { return false, nil }
	defer func() { passwordAlreadyCorrect = orig }()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	passwords := map[string]string{
		RoleMigrator: "migrator",
		RoleAdmin:    "admin",
		RoleRuntime:  "runtime",
	}
	mock.ExpectExec(`ALTER ROLE "orbitjob_migrator" PASSWORD 'migrator'`).WillReturnError(errors.New("denied"))

	err = EnsureRolePasswords(context.Background(), db, passwords)
	if err == nil || !strings.Contains(err.Error(), "set password for orbitjob_migrator") {
		t.Fatalf("EnsureRolePasswords() error = %v", err)
	}
}

func TestPasswordAlreadyCorrect_IntegrationGuard(t *testing.T) {
	// This unit test ensures the real passwordAlreadyCorrect function is
	// non-nil and compiles — the integration path is exercised by kind tests.
	if passwordAlreadyCorrect == nil {
		t.Fatal("passwordAlreadyCorrect must not be nil")
	}

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// With sqlmock, inet_server_addr() won't work, but we verify the
	// function returns an error rather than panicking.
	_, err = passwordAlreadyCorrect(context.Background(), db, "role", "pass")
	if err == nil {
		t.Fatal("expected error from sqlmock connection info query")
	}
}

func TestManagedLoginRoles(t *testing.T) {
	roles := ManagedLoginRoles()
	if len(roles) != 3 {
		t.Fatalf("expected 3 managed login roles, got %d", len(roles))
	}
	seen := map[string]bool{}
	for _, r := range roles {
		if seen[r] {
			t.Fatalf("duplicate role %q", r)
		}
		seen[r] = true
	}
	if !seen[RoleMigrator] || !seen[RoleAdmin] || !seen[RoleRuntime] {
		t.Fatalf("missing expected roles: %v", roles)
	}
}

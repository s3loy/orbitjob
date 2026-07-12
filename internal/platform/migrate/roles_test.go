package migrate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

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
		"orbitjob_migrator": "migrator",
		"orbitjob_admin":    "admin",
		"orbitjob_runtime":  "runtime",
	})
	if err == nil || !strings.Contains(err.Error(), "password for orbitjob_operator is required") {
		t.Fatalf("EnsureRolePasswords() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRolePasswordsUsesFixedAllowlist(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	passwords := map[string]string{
		"orbitjob_migrator": "migrator",
		"orbitjob_admin":    "admin",
		"orbitjob_runtime":  "runtime",
		"orbitjob_operator": "operator",
		"attacker":          "ignored",
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

func TestEnsureRolePasswordsPropagatesDatabaseError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	passwords := map[string]string{
		"orbitjob_migrator": "migrator",
		"orbitjob_admin":    "admin",
		"orbitjob_runtime":  "runtime",
		"orbitjob_operator": "operator",
	}
	mock.ExpectExec(`ALTER ROLE "orbitjob_migrator" PASSWORD 'migrator'`).WillReturnError(errors.New("denied"))

	err = EnsureRolePasswords(context.Background(), db, passwords)
	if err == nil || !strings.Contains(err.Error(), "set password for orbitjob_migrator") {
		t.Fatalf("EnsureRolePasswords() error = %v", err)
	}
}

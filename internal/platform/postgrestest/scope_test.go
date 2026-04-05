package postgrestest

import (
	"strings"
	"testing"
)

func TestSchemaNameForPath(t *testing.T) {
	got := schemaNameForPath("internal/admin/store/postgres")
	if !strings.HasPrefix(got, testSchemaPrefix) {
		t.Fatalf("expected schema prefix %q, got %q", testSchemaPrefix, got)
	}
	if len(got) > maxIdentifierLength {
		t.Fatalf("expected schema length <= %d, got %d (%q)", maxIdentifierLength, len(got), got)
	}
	if strings.Contains(got, "/") {
		t.Fatalf("expected sanitized schema name, got %q", got)
	}
}

func TestWithSearchPath(t *testing.T) {
	got, err := withSearchPath(
		"postgres://postgres:postgres@127.0.0.1:5432/orbitjob_test?sslmode=disable",
		"ojtest_admin",
	)
	if err != nil {
		t.Fatalf("withSearchPath() error = %v", err)
	}
	if !strings.Contains(got, "search_path=ojtest_admin%2Cpublic") {
		t.Fatalf("expected search_path to be appended, got %q", got)
	}
}

func TestSchemaNameFromDSN(t *testing.T) {
	got, err := schemaNameFromDSN(
		"postgres://postgres:postgres@127.0.0.1:5432/orbitjob_test?search_path=ojtest_core%2Cpublic&sslmode=disable",
	)
	if err != nil {
		t.Fatalf("schemaNameFromDSN() error = %v", err)
	}
	if got != "ojtest_core" {
		t.Fatalf("expected schema name %q, got %q", "ojtest_core", got)
	}
}

func TestTestDSN(t *testing.T) {
	got, schemaName, err := testDSN(
		"postgres://postgres:postgres@127.0.0.1:5432/orbitjob_test?search_path=ojtest_pkg%2Cpublic&sslmode=disable",
		"TestRepository/Create",
	)
	if err != nil {
		t.Fatalf("testDSN() error = %v", err)
	}
	if schemaName == "ojtest_pkg" {
		t.Fatalf("expected test-specific schema, got package schema %q", schemaName)
	}
	if !strings.Contains(got, "search_path=") {
		t.Fatalf("expected search_path in scoped test dsn, got %q", got)
	}
}

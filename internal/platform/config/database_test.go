package config

import (
	"strings"
	"testing"
)

func TestResolveDatabaseDSNUsesFirstConfiguredVariable(t *testing.T) {
	t.Setenv("RUNTIME_DSN", "postgres://runtime")
	t.Setenv("WORKER_DSN", "postgres://worker")
	t.Setenv("DATABASE_DSN", "postgres://legacy")

	got, source, err := ResolveDatabaseDSN("RUNTIME_DSN", "WORKER_DSN", "DATABASE_DSN")
	if err != nil {
		t.Fatalf("ResolveDatabaseDSN() error = %v", err)
	}
	if got != "postgres://runtime" {
		t.Fatalf("dsn = %q, want runtime DSN", got)
	}
	if source != "RUNTIME_DSN" {
		t.Fatalf("source = %q, want RUNTIME_DSN", source)
	}
}

func TestResolveDatabaseDSNFallsBackInOrder(t *testing.T) {
	t.Setenv("RUNTIME_DSN", "")
	t.Setenv("WORKER_DSN", "postgres://worker")
	t.Setenv("DATABASE_DSN", "postgres://legacy")

	got, source, err := ResolveDatabaseDSN("RUNTIME_DSN", "WORKER_DSN", "DATABASE_DSN")
	if err != nil {
		t.Fatalf("ResolveDatabaseDSN() error = %v", err)
	}
	if got != "postgres://worker" || source != "WORKER_DSN" {
		t.Fatalf("got (%q, %q), want worker DSN", got, source)
	}
}

func TestResolveDatabaseDSNListsAcceptedVariables(t *testing.T) {
	t.Setenv("ADMIN_DSN", "")
	t.Setenv("DATABASE_DSN", "")

	_, _, err := ResolveDatabaseDSN("ADMIN_DSN", "DATABASE_DSN")
	if err == nil || !strings.Contains(err.Error(), "ADMIN_DSN or DATABASE_DSN is required") {
		t.Fatalf("ResolveDatabaseDSN() error = %v", err)
	}
}

func TestResolveDatabaseDSNRejectsEmptyKeyList(t *testing.T) {
	_, _, err := ResolveDatabaseDSN()
	if err == nil || err.Error() != "database DSN variable list is empty" {
		t.Fatalf("ResolveDatabaseDSN() error = %v", err)
	}
}

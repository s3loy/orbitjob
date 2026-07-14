package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"orbitjob/internal/platform/dbconfig"
)

func TestParseOptionsDefaults(t *testing.T) {
	opts, err := parseOptions([]string{"setup"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Mode != dbconfig.ModeBundled || opts.StatePath != ".runtime/database.json" || opts.RuntimeEnvPath != ".runtime/database.env" {
		t.Fatalf("options = %#v", opts)
	}
}

func TestParseOptionsRejectsUnknownMode(t *testing.T) {
	if _, err := parseOptions([]string{"setup", "--mode", "unknown"}); err == nil {
		t.Fatal("expected mode error")
	}
}

func TestRunSetupBundledWritesConfiguration(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "database.json")
	envPath := filepath.Join(dir, "database.env")
	openCalls := 0
	deps := setupDeps{
		OpenDB: func(string) (*sql.DB, error) {
			openCalls++
			return nil, errors.New("OpenDB should not be called in bundled setup")
		},
		Random: bytes.NewReader(bytes.Repeat([]byte{3}, 96)),
		Out:    io.Discard,
	}
	err := runSetup(context.Background(), setupOptions{
		Mode: dbconfig.ModeBundled, StatePath: statePath, RuntimeEnvPath: envPath,
		BundledEndpoint: "postgres://pg:5432/orbitjob?sslmode=disable",
		BundledOwner:    dbconfig.Credentials{Username: "orbitjob", Password: "owner"},
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if openCalls != 0 {
		t.Fatalf("OpenDB calls = %d, want 0", openCalls)
	}
	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, key := range []string{"BOOTSTRAP_OWNER_DSN=", "MIGRATOR_DSN=", "ADMIN_DSN=", "RUNTIME_DSN=", "MIGRATOR_PASSWORD=", "ADMIN_PASSWORD=", "RUNTIME_PASSWORD="} {
		if !strings.Contains(text, key) {
			t.Fatalf("runtime env missing %s", key)
		}
	}
}

func TestRunSetupExternalReadsOneDSNFileAndRedactsOutput(t *testing.T) {
	dir := t.TempDir()
	dsnPath := filepath.Join(dir, "bootstrap-dsn")
	secret := "postgres://owner:secret@db:5432/orbitjob?sslmode=disable"
	if err := os.WriteFile(dsnPath, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	deps := setupDeps{OpenDB: func(string) (*sql.DB, error) { return nil, errors.New("stop") }, Random: rand.Reader, Out: &output}
	err := runSetup(context.Background(), setupOptions{Mode: dbconfig.ModeExternal, BootstrapDSNFile: dsnPath, StatePath: filepath.Join(dir, "state"), RuntimeEnvPath: filepath.Join(dir, "env")}, deps)
	if err == nil || !strings.Contains(err.Error(), "open bootstrap database") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(output.String(), "secret") || !strings.Contains(output.String(), "***") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRunSetupExistingStateIsNoop(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "database.json")
	installation, err := dbconfig.NewBundled("postgres://pg:5432/orbitjob", dbconfig.Credentials{Username: "owner", Password: "owner"}, bytes.NewReader(bytes.Repeat([]byte{4}, 96)))
	if err != nil {
		t.Fatal(err)
	}
	if err := dbconfig.WriteState(statePath, installation); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = runSetup(context.Background(), setupOptions{Mode: dbconfig.ModeBundled, StatePath: statePath, RuntimeEnvPath: filepath.Join(dir, "database.env")}, setupDeps{OpenDB: func(string) (*sql.DB, error) { t.Fatal("OpenDB called"); return nil, nil }, Random: rand.Reader, Out: &output})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Runtime configuration refreshed") {
		t.Fatalf("output = %q", output.String())
	}
}

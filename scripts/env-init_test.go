package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestGenerateValues(t *testing.T) {
	values, err := generateValues()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(values.PGPassword) {
		t.Fatalf("PG password format: %q", values.PGPassword)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(values.GrafanaPassword) {
		t.Fatalf("Grafana password format: %q", values.GrafanaPassword)
	}
	for name, value := range map[string]string{
		"migrator": values.MigratorPassword,
		"admin":    values.AdminPassword,
		"runtime":  values.RuntimePassword,
		"operator": values.OperatorPassword,
	} {
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(value) {
			t.Fatalf("%s password format: %q", name, value)
		}
	}
	if !regexp.MustCompile(`^otj_[0-9a-f]{48}$`).MatchString(values.BootstrapAPIKey) {
		t.Fatalf("bootstrap key format: %q", values.BootstrapAPIKey)
	}
}

func TestWriteEnvCreatesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	values := Values{PGPassword: strings.Repeat("a", 64), MigratorPassword: strings.Repeat("d", 64), AdminPassword: strings.Repeat("e", 64), RuntimePassword: strings.Repeat("f", 64), OperatorPassword: strings.Repeat("1", 64), GrafanaPassword: strings.Repeat("b", 64), BootstrapAPIKey: "otj_" + strings.Repeat("c", 48)}
	if err := writeEnv(path, values); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}

func TestWriteEnvRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := writeEnv(path, Values{})
	if err == nil {
		t.Fatal("expected overwrite error")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "keep\n" {
		t.Fatalf("existing file changed: %q", data)
	}
}

func TestEnsureEnvTightensExistingFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "PG_PASSWORD=" + strings.Repeat("a", 64) + "\nMIGRATOR_PASSWORD=" + strings.Repeat("d", 64) + "\nADMIN_PASSWORD=" + strings.Repeat("e", 64) + "\nRUNTIME_PASSWORD=" + strings.Repeat("f", 64) + "\nOPERATOR_PASSWORD=" + strings.Repeat("1", 64) + "\nGRAFANA_PASSWORD=" + strings.Repeat("b", 64) + "\nADMIN_BOOTSTRAP_API_KEY=otj_" + strings.Repeat("c", 48) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureEnv(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
	data, _ := os.ReadFile(path)
	if string(data) != content {
		t.Fatal("ensureEnv changed existing values")
	}
}

func TestValidateEnvRejectsMissingAndShortValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("PG_PASSWORD=x\nGRAFANA_PASSWORD=\nADMIN_BOOTSTRAP_API_KEY=short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateEnv(path); err == nil {
		t.Fatal("expected validation error")
	}
}

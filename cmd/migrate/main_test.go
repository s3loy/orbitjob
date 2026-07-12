package main

import (
	"os"
	"strings"
	"testing"
)

func TestLoadConfigRequiresMode(t *testing.T) {
	t.Setenv("MIGRATION_MODE", "")
	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "MIGRATION_MODE") {
		t.Fatalf("loadConfig() error = %v", err)
	}
}

func TestLoadConfigOwnerInit(t *testing.T) {
	setMigrationEnv(t)
	t.Setenv("MIGRATION_MODE", string(modeOwnerInit))
	t.Setenv("BOOTSTRAP_OWNER_DSN", "postgres://owner")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.mode != modeOwnerInit || cfg.dsn != "postgres://owner" {
		t.Fatalf("loadConfig() = %#v", cfg)
	}
}

func TestLoadConfigMigrate(t *testing.T) {
	t.Setenv("MIGRATION_MODE", string(modeMigrate))
	t.Setenv("MIGRATOR_DSN", "postgres://migrator")
	t.Setenv("MIGRATIONS_BASELINE_VERSION", "2")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.mode != modeMigrate || cfg.dsn != "postgres://migrator" || cfg.baselineVersion != 2 {
		t.Fatalf("loadConfig() = %#v", cfg)
	}
}

func TestLoadConfigRejectsDatabaseDSNFallback(t *testing.T) {
	t.Setenv("MIGRATION_MODE", string(modeMigrate))
	t.Setenv("MIGRATOR_DSN", "")
	t.Setenv("DATABASE_DSN", "postgres://owner")

	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "MIGRATOR_DSN") {
		t.Fatalf("loadConfig() error = %v", err)
	}
}

func TestLoadConfigRejectsInvalidBaseline(t *testing.T) {
	t.Setenv("MIGRATION_MODE", string(modeMigrate))
	t.Setenv("MIGRATOR_DSN", "postgres://migrator")
	t.Setenv("MIGRATIONS_BASELINE_VERSION", "-1")

	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "non-negative integer") {
		t.Fatalf("loadConfig() error = %v", err)
	}
}

func setMigrationEnv(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"MIGRATOR_PASSWORD": "migrator",
		"ADMIN_PASSWORD":    "admin",
		"RUNTIME_PASSWORD":  "runtime",
		"OPERATOR_PASSWORD": "operator",
	} {
		t.Setenv(key, value)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

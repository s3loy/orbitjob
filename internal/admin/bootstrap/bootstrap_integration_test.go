//go:build integration

package bootstrap

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"orbitjob/internal/platform/migrate"
	"orbitjob/internal/platform/postgrestest"
)

// itestAdminPassword is the password the integration suites provision for the
// deployment roles. Reusing the http suite's value means the two suites never
// fight over a role password on a shared cluster.
const itestAdminPassword = "itest-admin"

// openAdminDB returns the two handles the bootstrap suite needs.
//
// postgrestest.Open builds this test's schema and then truncates every table
// for a clean slate, which also wipes the platform preset policies the
// baseline seeds -- presets the schema's own contract expects. And the
// baseline lets orbitjob_bootstrap_default execute only for the orbitjob_admin
// identity, so bootstrap has to run on that role rather than on the superuser
// handle the schema was built with.
//
// openAdminDB provisions the deployment roles' passwords, replays the preset
// seed, and opens an orbitjob_admin connection scoped to this test's schema.
// The superuser handle stays for direct SQL assertions: reading rows back
// relies on the superuser's RLS bypass, since the admin connection sees rows
// only through app.tenant_id.
func openAdminDB(t *testing.T) (adminDB *sql.DB, db *sql.DB) {
	t.Helper()
	db = postgrestest.Open(t)

	ctx := context.Background()

	// Provision the deployment role's password, then open bootstrap's handle
	// as that role, keeping this test's schema as the search path.
	if err := migrate.EnsureRolePasswords(ctx, db, map[string]string{
		migrate.RoleMigrator: "itest-migrator",
		migrate.RoleAdmin:    itestAdminPassword,
		migrate.RoleRuntime:  "itest-runtime",
	}); err != nil {
		t.Fatalf("provision deployment role passwords: %v", err)
	}
	var searchPath string
	if err := db.QueryRowContext(ctx, "SHOW search_path").Scan(&searchPath); err != nil {
		t.Fatalf("read test search path: %v", err)
	}
	parsed, err := url.Parse(os.Getenv("TEST_DATABASE_DSN"))
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_DSN: %v", err)
	}
	parsed.User = url.UserPassword(migrate.RoleAdmin, itestAdminPassword)
	query := parsed.Query()
	query.Set("search_path", searchPath)
	parsed.RawQuery = query.Encode()

	adminDB, err = sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatalf("open orbitjob_admin connection: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if err := adminDB.PingContext(ctx); err != nil {
		t.Fatalf("ping orbitjob_admin connection: %v", err)
	}

	// Restore the presets the schema harness truncated away, so the bootstrap
	// function finds the rows the schema's own contract guarantees. Seeding
	// runs on the superuser handle: a preset carries no tenant id, and the RLS
	// policy on policies governs tenant-owned rows, not platform ones.
	if err := postgrestest.SeedPresetPolicies(ctx, db); err != nil {
		t.Fatalf("seed platform preset policies: %v", err)
	}
	return adminDB, db
}

func TestEnsureDefault_FirstRunCreatesTenantAndKey(t *testing.T) {
	adminDB, db := openAdminDB(t)
	tmp := t.TempDir()
	writer := &LocalFileSecretWriter{Root: tmp}

	res, err := EnsureDefault(context.Background(), adminDB, Options{
		APIKey: "otj_bootstrap_first_run",
		Writer: writer,
	})
	if err != nil {
		t.Fatalf("EnsureDefault error = %v", err)
	}
	if !res.TenantCreated {
		t.Fatal("expected tenant created on first run")
	}
	if !res.KeyCreated {
		t.Fatal("expected key created on first run")
	}
	if res.MaskedKey == "" {
		t.Fatal("expected masked key on first run")
	}

	path := filepath.Join(tmp, defaultSecretName, defaultSecretDataKey)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read secret file: %v", err)
	}
	if string(data) != "otj_bootstrap_first_run" {
		t.Fatalf("unexpected secret data: %s", string(data))
	}

	var tenantCount int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM tenants WHERE id = $1", DefaultTenantID).Scan(&tenantCount); err != nil {
		t.Fatalf("count tenants: %v", err)
	}
	if tenantCount != 1 {
		t.Fatalf("expected 1 default tenant, got %d", tenantCount)
	}

	var keyCount int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM api_keys WHERE id = $1", DefaultAPIKeyID).Scan(&keyCount); err != nil {
		t.Fatalf("count api keys: %v", err)
	}
	if keyCount != 1 {
		t.Fatalf("expected 1 default api key, got %d", keyCount)
	}
}

func TestEnsureDefault_SecondRunIsIdempotent(t *testing.T) {
	adminDB, _ := openAdminDB(t)
	ctx := context.Background()

	if _, err := EnsureDefault(ctx, adminDB, Options{APIKey: "otj_bootstrap_idempotent"}); err != nil {
		t.Fatalf("first EnsureDefault error = %v", err)
	}

	res, err := EnsureDefault(ctx, adminDB, Options{APIKey: "otj_bootstrap_idempotent"})
	if err != nil {
		t.Fatalf("second EnsureDefault error = %v", err)
	}
	if res.TenantCreated {
		t.Fatal("expected tenant not created on second run")
	}
	if res.KeyCreated {
		t.Fatal("expected key not created on second run")
	}
}

func TestEnsureDefault_LocalFileSecretWriterPath(t *testing.T) {
	tmp := t.TempDir()
	writer := &LocalFileSecretWriter{Root: tmp}

	ctx := context.Background()
	if err := writer.Write(ctx, "my-secret", map[string]string{"token": "abc123"}); err != nil {
		t.Fatalf("Write error = %v", err)
	}

	want := filepath.Join(tmp, "my-secret", "token")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read secret file: %v", err)
	}
	if string(data) != "abc123" {
		t.Fatalf("unexpected file content: %s", string(data))
	}
}

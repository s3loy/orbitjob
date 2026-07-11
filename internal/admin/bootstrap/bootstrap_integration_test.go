//go:build integration

package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"orbitjob/internal/platform/postgrestest"
)

func TestEnsureDefault_FirstRunCreatesTenantAndKey(t *testing.T) {
	db := postgrestest.Open(t)
	tmp := t.TempDir()
	writer := &LocalFileSecretWriter{Root: tmp}

	res, err := EnsureDefault(context.Background(), db, Options{
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
	db := postgrestest.Open(t)
	ctx := context.Background()

	if _, err := EnsureDefault(ctx, db, Options{APIKey: "otj_bootstrap_idempotent"}); err != nil {
		t.Fatalf("first EnsureDefault error = %v", err)
	}

	res, err := EnsureDefault(ctx, db, Options{APIKey: "otj_bootstrap_idempotent"})
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

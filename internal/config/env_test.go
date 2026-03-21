package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFindDotenvFrom(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "internal", "store", "postgres")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	envPath := filepath.Join(root, ".env")
	if err := os.WriteFile(envPath, []byte("TEST_DATABASE_DSN=postgres://example"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := findDotenvFrom(nested, ".env")
	if err != nil {
		t.Fatalf("findDotenvFrom() error = %v", err)
	}
	if got != envPath {
		t.Fatalf("expected path=%q, got %q", envPath, got)
	}
}

func TestFindDotenvFrom_NotFound(t *testing.T) {
	root := t.TempDir()

	_, err := findDotenvFrom(root, ".env")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

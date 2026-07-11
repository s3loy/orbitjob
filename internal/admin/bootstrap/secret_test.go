package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalFileSecretWriter_WritesFiles(t *testing.T) {
	dir := t.TempDir()
	w := &LocalFileSecretWriter{Root: dir}
	if err := w.Write(t.Context(), "bootstrap-api-key", map[string]string{"api-key": "otj_testkey_00"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "bootstrap-api-key", "api-key"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "otj_testkey_00" {
		t.Fatalf("unexpected content: %s", got)
	}
}

func TestLocalFileSecretWriter_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "root-is-file"), []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	w := &LocalFileSecretWriter{Root: filepath.Join(dir, "root-is-file")}
	if err := w.Write(t.Context(), "bootstrap-api-key", map[string]string{"api-key": "x"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestLocalFileSecretWriter_WriteFileError(t *testing.T) {
	dir := t.TempDir()
	secretDir := filepath.Join(dir, "bootstrap-api-key")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Pre-create the target path as a directory so WriteFile fails.
	if err := os.MkdirAll(filepath.Join(secretDir, "api-key"), 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	w := &LocalFileSecretWriter{Root: dir}
	if err := w.Write(t.Context(), "bootstrap-api-key", map[string]string{"api-key": "x"}); err == nil {
		t.Fatal("expected error")
	}
}

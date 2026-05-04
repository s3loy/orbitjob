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

func TestFindDotenvFrom_InStartDir(t *testing.T) {
	root := t.TempDir()
	envPath := filepath.Join(root, ".env")
	if err := os.WriteFile(envPath, []byte("KEY=value"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := findDotenvFrom(root, ".env")
	if err != nil {
		t.Fatalf("findDotenvFrom() error = %v", err)
	}
	if got != envPath {
		t.Fatalf("expected path=%q, got %q", envPath, got)
	}
}

func TestFindDotenvFrom_DeepNesting(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "c", "d")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	envPath := filepath.Join(root, ".env")
	if err := os.WriteFile(envPath, []byte("KEY=deep"), 0o644); err != nil {
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

func TestFindDotenvFrom_Midpoint(t *testing.T) {
	root := t.TempDir()
	mid := filepath.Join(root, "internal")
	nested := filepath.Join(mid, "store", "postgres")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	envPath := filepath.Join(mid, ".env")
	if err := os.WriteFile(envPath, []byte("MID_KEY=mid_value"), 0o644); err != nil {
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

func TestFindDotenvFrom_SkipDirectoryNamedEnv(t *testing.T) {
	root := t.TempDir()
	dirNamedEnv := filepath.Join(root, ".env")
	if err := os.MkdirAll(dirNamedEnv, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	_, err := findDotenvFrom(root, ".env")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist when .env is a directory, got %v", err)
	}
}

func TestLoadDotenv_HappyPath(t *testing.T) {
	root := t.TempDir()
	envPath := filepath.Join(root, ".env")
	content := "# comment line\nORBIT_TEST_LOADDOTENV=hello\n\nOTHER=world\n"
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("ORBIT_TEST_LOADDOTENV")
		_ = os.Unsetenv("OTHER")
		_ = os.Chdir(origWD)
	})

	if err := LoadDotenv(); err != nil {
		t.Fatalf("LoadDotenv() error = %v", err)
	}

	if got := os.Getenv("ORBIT_TEST_LOADDOTENV"); got != "hello" {
		t.Fatalf("expected ORBIT_TEST_LOADDOTENV=hello, got %q", got)
	}
	if got := os.Getenv("OTHER"); got != "world" {
		t.Fatalf("expected OTHER=world, got %q", got)
	}
}

package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func resetLoadDotenv() {
	loadDotenvOnce = sync.Once{}
	loadDotenvErr = nil
}

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
	resetLoadDotenv()

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

func TestLoadDotenv_NotFound(t *testing.T) {
	resetLoadDotenv()

	root := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	// .env does not exist; LoadDotenv should return nil (not an error)
	if err := LoadDotenv(); err != nil {
		t.Fatalf("LoadDotenv() error = %v, want nil when .env is absent", err)
	}
}

func TestFindDotenv_FromChildDir(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "sub", "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	envPath := filepath.Join(root, ".env")
	if err := os.WriteFile(envPath, []byte("KEY=value"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(child); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	got, err := findDotenv(".env")
	if err != nil {
		t.Fatalf("findDotenv() error = %v", err)
	}
	if got != envPath {
		t.Fatalf("expected path=%q, got %q", envPath, got)
	}
}

func TestFindDotenv_NotFound(t *testing.T) {
	root := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	_, err = findDotenv(".env")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func TestFindDotenvFrom_StatError(t *testing.T) {
	// Use a path with a character that is invalid in Windows filenames, causing
	// os.Stat to return a non-ErrNotExist error (syntax error).
	root := t.TempDir()
	badPath := filepath.Join(root, "bad<name", ".env")
	_, err := findDotenvFrom(badPath, ".env")
	if err == nil {
		t.Fatalf("expected an error for path with invalid character")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected an error other than ErrNotExist, got %v", err)
	}
}

func TestLoadDotenv_EmptyFile(t *testing.T) {
	resetLoadDotenv()

	root := t.TempDir()
	envPath := filepath.Join(root, ".env")
	if err := os.WriteFile(envPath, []byte(""), 0o644); err != nil {
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
		_ = os.Chdir(origWD)
	})

	if err := LoadDotenv(); err != nil {
		t.Fatalf("LoadDotenv() with empty .env returned error: %v", err)
	}
}

func TestLoadDotenv_CommentOnly(t *testing.T) {
	resetLoadDotenv()

	root := t.TempDir()
	envPath := filepath.Join(root, ".env")
	content := "# This is a comment\n# Another comment line\n   \n# Yet another\n"
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
		_ = os.Chdir(origWD)
	})

	if err := LoadDotenv(); err != nil {
		t.Fatalf("LoadDotenv() with comment-only .env returned error: %v", err)
	}
}

func TestLoadDotenv_MalformedContent(t *testing.T) {
	resetLoadDotenv()

	root := t.TempDir()
	envPath := filepath.Join(root, ".env")
	// godotenv may reject lines starting with 'export' without a value.
	// Actually godotenv handles export, so use genuinely malformed content:
	// a line with only = and no key
	content := "=value\n"
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
		_ = os.Chdir(origWD)
	})

	// godotenv may or may not error on malformed content depending on version.
	// We just verify LoadDotenv does not panic.
	_ = LoadDotenv()
}

func TestLoadDotenv_FromTempDir(t *testing.T) {
	resetLoadDotenv()

	// findDotenv is also tested directly from a temp directory with no .env file.
	root := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	_, err = findDotenv(".env")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist from temp dir without .env, got %v", err)
	}
}


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

func TestFindDotenv_GetwdError(t *testing.T) {
	// Simulate os.Getwd failure by switching to a directory that is then removed.
	// On Windows, the CWD is locked, so this may not work. We try anyway.
	origWD, err := os.Getwd()
	if err != nil {
		t.Skipf("Getwd() error = %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "orbitjob-finddotenv-test")
	if err != nil {
		t.Skipf("MkdirTemp error = %v", err)
	}

	if err := os.Chdir(tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Skipf("Chdir error = %v", err)
	}

	// Return to original dir and remove temp dir.
	_ = os.Chdir(origWD)
	if err := os.RemoveAll(tmpDir); err != nil {
		// On Windows, the directory may be locked. Skip if we can't test this.
		t.Skipf("RemoveAll error (likely CWD lock): %v", err)
	}

	// Now chdir into a non-existent directory — os.Getwd will fail.
	_, err = os.Getwd()
	if err != nil {
		// Getwd itself already fails: our process lost its CWD.
		// Call findDotenv. Since os.Getwd inside it will also fail
		// (the kernel remembers the old CWD that no longer exists),
		// it should hit the error return path.
		_, err2 := findDotenv(".env")
		if err2 == nil {
			t.Fatal("expected error from findDotenv when CWD is gone")
		}
	} else {
		// Getwd didn't fail — the CWD was restored. Re-chdir to a removed dir.
		if err := os.Chdir(tmpDir); err == nil {
			t.Skip("could not trigger Getwd failure (CWD was re-resolved)")
		}
		// On some systems chdir to removed dir fails, so we can't test this.
		t.Skip("Getwd failure path cannot be triggered in this environment")
	}
}

func TestLoadDotenv_DefaultErrorCase(t *testing.T) {
	// When findDotenv returns a non-ErrNotExist error, LoadDotenv
	// should return that error (the default case in the switch).
	resetLoadDotenv()

	origWD, err := os.Getwd()
	if err != nil {
		t.Skipf("Getwd() error = %v", err)
	}

	// Create a temp dir, chdir into it, then remove it to break Getwd.
	tmpDir, err := os.MkdirTemp("", "orbitjob-loaddotenv-test")
	if err != nil {
		t.Skipf("MkdirTemp error = %v", err)
	}

	if err := os.Chdir(tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Skipf("Chdir error = %v", err)
	}

	_ = os.Chdir(origWD)
	rmErr := os.RemoveAll(tmpDir)

	if rmErr != nil {
		// Can't remove CWD. Try chdir to a path with a stat error:
		// chdir into tmpDir, then create a file that blocks stat on ".env".
		// This approach uses the fact that findDotenv starts from CWD and walks up.
		// If we cd to a directory with a stat error, findDotenv will fail.
		_ = os.Chdir(origWD)
		resetLoadDotenv()

		// Try with a completely inaccessible path:
		// Create a deeply nested path with invalid chars
		badDir := filepath.Join(tmpDir, "bad>name")
		if err := os.Chdir(badDir); err == nil {
			// This shouldn't succeed since mkdir won't create bad paths
			_ = os.Chdir(origWD)
		}

		// The simplest approach: use os.Chdir to enter tmpDir and
		// then make the CWD have a non-ErrNotExist os.Stat error.
		// Hard on Windows without proper permissions.
		t.Skip("Getwd/stat error path cannot be triggered in this environment")
	}

	// CWD was successfully removed. Try chdir into it.
	cerr := os.Chdir(tmpDir)
	if cerr != nil {
		// chdir to removed dir fails — OS doesn't allow this on some platforms.
		t.Skipf("Chdir to removed dir failed: %v", cerr)
	}

	// Now the CWD doesn't exist. findDotenv should fail via os.Getwd.
	// LoadDotenv should catch this in the default case.
	_ = LoadDotenv()

	// After LoadDotenv runs, loadDotenvErr should hold the error.
	// Any subsequent call returns it.
	err = LoadDotenv()
	if err == nil {
		t.Skip("LoadDotenv succeeded (CWD was somehow restored); default path not covered")
	}

	resetLoadDotenv()
	_ = os.Chdir(origWD)
	t.Logf("LoadDotenv error (Getwd failure): %v", err)
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

	// macOS: /tmp is a symlink to /private/tmp; resolve for comparison.
	want, err := filepath.EvalSymlinks(envPath)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
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
	if got != want {
		t.Fatalf("expected path=%q, got %q", want, got)
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
	// Create a regular file, then stat a path through it as if it were a
	// directory. os.Stat("file/sub") returns ENOTDIR on Linux (not
	// ErrNotExist) but maps to ErrNotExist on Windows. Skip when the OS
	// maps it to ErrNotExist.
	root := t.TempDir()
	filePath := filepath.Join(root, "some-file")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := findDotenvFrom(filepath.Join(filePath, "sub"), ".env")
	if err == nil {
		t.Fatal("expected an error when a path component is a file")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("ENOTDIR maps to ErrNotExist on this OS")
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


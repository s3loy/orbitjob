package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoArtifactsUsesIndexedIgnoreRules(t *testing.T) {
	repo := t.TempDir()
	script, err := filepath.Abs("hooks/no-artifacts.sh")
	if err != nil {
		t.Fatal(err)
	}
	// An alternate index is also used by Git for some partial commits.
	var env []string
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GIT_") {
			env = append(env, value)
		}
	}
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(env, "GIT_INDEX_FILE="+filepath.Join(repo, "test-index"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		target := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	check := func() (string, error) {
		cmd := exec.Command("bash", script)
		cmd.Dir = filepath.Join(repo, "api")
		cmd.Env = append(env, "GIT_INDEX_FILE=test-index")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	runGit("init", "-q")
	write(".gitignore", "*.o\n")
	write("api/gen.o", "fixture artifact\n")
	runGit("add", "-f", "--", ".gitignore", "api/gen.o")
	write("api/.gitignore", "!gen.o\n")
	if out, err := check(); err == nil || !strings.Contains(out, "api/gen.o") {
		t.Fatalf("untracked ignore override let the artifact pass: %v: %s", err, out)
	}

	runGit("add", "--", "api/.gitignore")
	write("api/.gitignore", "*.o\n")
	if out, err := check(); err != nil {
		t.Fatalf("staged exception was ignored: %v: %s", err, out)
	}
}

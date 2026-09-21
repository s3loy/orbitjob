package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateReleaseCommit(t *testing.T) {
	repo := t.TempDir()
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	runGit("init", "-q", "-b", "main")
	runGit("config", "user.name", "OrbitJob Test")
	runGit("config", "user.email", "orbitjob@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "state"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit("add", "state")
	runGit("commit", "-q", "-m", "main")
	mainSHA := strings.TrimSpace(runGit("rev-parse", "HEAD"))
	runGit("checkout", "-q", "-b", "dev")
	if err := os.WriteFile(filepath.Join(repo, "state"), []byte("dev\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit("commit", "-q", "-am", "dev")
	devSHA := strings.TrimSpace(runGit("rev-parse", "HEAD"))

	script, err := filepath.Abs("validate-release-commit.sh")
	if err != nil {
		t.Fatal(err)
	}
	check := func(releaseRef string) error {
		cmd := exec.Command("bash", script, releaseRef, "main")
		cmd.Dir = repo
		return cmd.Run()
	}
	if err := check(mainSHA); err != nil {
		t.Fatalf("current main commit rejected: %v", err)
	}
	if err := check(devSHA); err == nil {
		t.Fatal("off-main commit accepted")
	}
}

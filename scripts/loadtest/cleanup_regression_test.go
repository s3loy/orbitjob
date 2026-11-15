package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupRejectsTraversalAndSelectorSyntax(t *testing.T) {
	for _, id := range []string{"", ".", "..", "../../../scripts", "/tmp/run", `..\run`, "run/child", "*", "run,other", "run=other", "a b", "-run", "run-"} {
		if err := ValidateCleanRunID(id); err == nil {
			t.Errorf("accepted unsafe run ID %q", id)
		}
	}
}

func TestCleanDeletesOnlyRequestedLocalArtifacts(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"run-one", "run-two"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	bin := t.TempDir()
	marker := filepath.Join(root, "cluster-command")
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte("#!/bin/sh\nprintf called > \"$CLEANUP_MARKER\"\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLEANUP_MARKER", marker)
	t.Setenv("PATH", bin)
	if err := runClean([]string{"--run-id", "run-one", "--run-root", root, "--confirm"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "run-one")); !os.IsNotExist(err) {
		t.Errorf("requested artifacts were not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "run-two")); err != nil {
		t.Errorf("unrelated run was affected: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("clean attempted shared cluster resource deletion")
	}
}

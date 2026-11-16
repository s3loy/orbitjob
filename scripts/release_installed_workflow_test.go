package main

import (
	"strings"
	"testing"
)

func TestReleaseInstallationRequiresCompleteConsoleSuite(t *testing.T) {
	job := readReleaseWorkflow(t, "verification.yml").Jobs["installation"]
	commands := []string{
		"kind create cluster",
		"make kind-verify",
		"python3 scripts/console-installed.py prepare",
		"python3 scripts/console-installed.py run",
	}
	last := -1
	for _, command := range commands {
		found := -1
		for i, step := range job.Steps {
			if strings.Contains(step.Run, command) {
				if step.If != "" || step.ContinueOnError {
					t.Fatalf("installed verification can skip %q", command)
				}
				found = i
				if strings.Contains(command, "console-installed.py run") && strings.Contains(step.Run, "--spec") {
					t.Fatal("release verification selects only part of the installed suite")
				}
				break
			}
		}
		if found <= last {
			t.Fatalf("missing or out-of-order installed verification command %q", command)
		}
		last = found
	}
	for _, command := range []string{"python3 scripts/console-installed.py cleanup", "kind delete cluster"} {
		found := false
		for i, step := range job.Steps {
			if strings.Contains(step.Run, command) && step.If == "always()" && i > last {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing unconditional cleanup after verification: %q", command)
		}
	}
}

func TestInstalledVerificationUploadsOnlySanitizedReceipt(t *testing.T) {
	job := readReleaseWorkflow(t, "verification.yml").Jobs["installation"]
	uploads := 0
	for _, step := range job.Steps {
		if !strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
			continue
		}
		uploads++
		if step.With["path"] != "${{ runner.temp }}/console-installed/verification.json" {
			t.Fatalf("installed verification may upload private state: %q", step.With["path"])
		}
	}
	if uploads != 1 {
		t.Fatalf("expected one sanitized receipt artifact, got %d", uploads)
	}
}

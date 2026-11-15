package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHostedSummaryCannotBorrowAnOlderPass(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/loadtest.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name, Run string
				Env       map[string]string
			}
		}
	}
	if err := yaml.Unmarshal(source, &workflow); err != nil {
		t.Fatal(err)
	}
	var block string
	for _, step := range workflow.Jobs["loadtest"].Steps {
		if step.Name == "Write the job summary" {
			block = step.Run
			if step.Env["RUN_DIR"] != "${{ steps.execution.outputs.run_dir }}" {
				t.Fatal("summary is not bound to the attempted run")
			}
		}
	}
	if block == "" {
		t.Fatal("summary step missing")
	}
	block = strings.ReplaceAll(block, "${{ job.status }}", "failure")
	for _, runDir := range []string{"", "test/load/runs/current"} {
		t.Run("run="+runDir, func(t *testing.T) {
			root := t.TempDir()
			old := filepath.Join(root, "test/load/runs/older")
			if err := os.MkdirAll(old, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(old, "result.json"), []byte(`{"verdict":"PASS"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(old, "report.md"), []byte("STALE SUCCESS"), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", block)
			cmd.Dir = root
			path := filepath.Join(root, "summary.md")
			cmd.Env = append(os.Environ(), "RUN_DIR="+runDir, "PROFILE=smoke", "RUN_URL=https://example.invalid/run", "GITHUB_STEP_SUMMARY="+path)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("summary: %v %s", err, out)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), "STALE SUCCESS") || strings.Contains(string(body), "| PASS |") || !strings.Contains(string(body), "none (job ended before the verifier ran)") {
				t.Fatalf("stale summary: %s", body)
			}
		})
	}
}

func TestHostedRunnerRecordsAttemptBeforeSetupCanFail(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"kubectl": "exit 1", "date": "echo 20261024T000000Z"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(root, "outputs")
	cmd := exec.Command("bash", "../loadtest-ci.sh", "smoke")
	cmd.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"), "GITHUB_OUTPUT="+output)
	if err := cmd.Run(); err == nil {
		t.Fatal("broken installation setup passed")
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "run_dir=test/load/runs/ci-smoke-20261024T000000Z\n" {
		t.Fatalf("lost attempted run identity: %q %v", data, err)
	}
}

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type releaseWorkflow struct {
	Jobs map[string]releaseJob `yaml:"jobs"`
}

type releaseJob struct {
	Uses            string            `yaml:"uses"`
	Needs           []string          `yaml:"needs"`
	With            map[string]string `yaml:"with"`
	If              string            `yaml:"if"`
	ContinueOnError bool              `yaml:"continue-on-error"`
	Steps           []releaseStep     `yaml:"steps"`
}

type releaseStep struct {
	Uses            string            `yaml:"uses"`
	Run             string            `yaml:"run"`
	With            map[string]string `yaml:"with"`
	If              string            `yaml:"if"`
	ContinueOnError bool              `yaml:"continue-on-error"`
}

// Image publication must have mandatory paths to every release gate. Reading
// the workflow graph catches a detached or conditional gate that can look green
// while publication proceeds without running it.
func TestImagePublicationRequiresCompleteVerification(t *testing.T) {
	for _, name := range []string{"release.yml", "prerelease.yml"} {
		t.Run(name, func(t *testing.T) {
			workflow := readReleaseWorkflow(t, name)
			images := workflow.Jobs["images"]
			if !slices.Contains(images.Needs, "verify") {
				t.Fatal("image publication does not depend on verification")
			}
			verify := workflow.Jobs["verify"]
			if verify.Uses != "./.github/workflows/verification.yml" || verify.If != "" || verify.ContinueOnError {
				t.Fatalf("verification is missing or optional: %+v", verify)
			}
			wantRef := "${{ github.sha }}"
			if name == "release.yml" {
				wantRef = "${{ needs.resolve.outputs.sha }}"
			}
			if verify.With["source_ref"] != wantRef {
				t.Fatalf("verification source=%q, want %q", verify.With["source_ref"], wantRef)
			}
		})
	}

	verify := readReleaseWorkflow(t, "verification.yml")
	installation := verify.Jobs["installation"]
	for _, gate := range []string{"backend", "postgres", "vulnerabilities", "console"} {
		job, ok := verify.Jobs[gate]
		if !ok || !slices.Contains(installation.Needs, gate) || job.If != "" || job.ContinueOnError {
			t.Fatalf("installation can bypass %s", gate)
		}
	}
	requireWorkflowCommand(t, verify.Jobs["backend"], "make check")
	requireWorkflowCommand(t, verify.Jobs["backend"], "make test-cover-check")
	requireWorkflowCommand(t, verify.Jobs["backend"], "make helm-check")
	requireWorkflowCommand(t, installation, "make kind-verify")
	if installation.If != "" || installation.ContinueOnError {
		t.Fatal("installation verification is optional")
	}
	for gate, workflow := range map[string]string{"postgres": "integration.yml", "vulnerabilities": "govulncheck.yml", "console": "console.yml"} {
		job := verify.Jobs[gate]
		if job.Uses != "./.github/workflows/"+workflow || job.With["source_ref"] != "${{ inputs.source_ref }}" {
			t.Fatalf("%s gate does not verify the selected source", gate)
		}
	}
	requireWorkflowCommand(t, readReleaseWorkflow(t, "integration.yml").Jobs["postgres"], "make integration")
	requireWorkflowCommand(t, readReleaseWorkflow(t, "govulncheck.yml").Jobs["govulncheck"], "govulncheck ./...")
}

func TestConsoleVerificationIsRequiredByCI(t *testing.T) {
	ci := readReleaseWorkflow(t, "ci.yml")
	if ci.Jobs["console"].Uses != "./.github/workflows/console.yml" || !slices.Contains(ci.Jobs["gate"].Needs, "console") {
		t.Fatal("CI gate does not require Console verification")
	}
	console := readReleaseWorkflow(t, "console.yml").Jobs["console"]
	for _, command := range []string{"npm ci", "npm run check", "npm run format:check", "npm run test:browser"} {
		requireWorkflowCommand(t, console, command)
	}
}

func TestCIGateRejectsFailedOrSkippedConsole(t *testing.T) {
	gate := readReleaseWorkflow(t, "ci.yml").Jobs["gate"]
	var command string
	for _, step := range gate.Steps {
		command += step.Run + "\n"
	}
	for _, tc := range []struct {
		docker, console string
		pass            bool
	}{
		{"success", "success", true},
		{"success", "failure", false},
		{"success", "skipped", false},
		{"success", "cancelled", false},
		{"failure", "success", false},
	} {
		cmd := exec.Command("bash", "-e", "-c", command)
		cmd.Env = append(gateEnvironment("RESULT", "CONSOLE_RESULT"), "RESULT="+tc.docker, "CONSOLE_RESULT="+tc.console)
		out, err := cmd.CombinedOutput()
		if (err == nil) != tc.pass {
			t.Fatalf("docker=%s console=%s pass=%v: %v %s", tc.docker, tc.console, tc.pass, err, out)
		}
	}
}

func TestReusableVerificationChecksOutSelectedSource(t *testing.T) {
	for _, name := range []string{"verification.yml", "console.yml", "integration.yml", "govulncheck.yml"} {
		workflow := readReleaseWorkflow(t, name)
		for id, job := range workflow.Jobs {
			for _, step := range job.Steps {
				if strings.HasPrefix(step.Uses, "actions/checkout@") && !strings.Contains(step.With["ref"], "inputs.source_ref") {
					t.Fatalf("%s job %s does not check out the selected source", name, id)
				}
			}
		}
	}
}

func readReleaseWorkflow(t *testing.T, name string) releaseWorkflow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var workflow releaseWorkflow
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow
}

func requireWorkflowCommand(t *testing.T, job releaseJob, command string) {
	t.Helper()
	for _, step := range job.Steps {
		if strings.Contains(step.Run, command) && step.If == "" && !step.ContinueOnError {
			return
		}
	}
	t.Fatalf("missing mandatory command %q", command)
}

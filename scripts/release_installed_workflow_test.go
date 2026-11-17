package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestReleaseInstallationRequiresCompleteConsoleSuite(t *testing.T) {
	job := readReleaseWorkflow(t, "verification.yml").Jobs["installation"]
	last, err := validateInstalledVerificationSequence(job)
	if err != nil {
		t.Fatal(err)
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

func validateInstalledVerificationSequence(job releaseJob) (int, error) {
	commands := []string{
		"kind create cluster",
		"make kind-verify",
		"helm upgrade orbitjob",
		"python3 scripts/console-installed.py prepare",
		"python3 scripts/console-installed.py run",
	}
	last := -1
	for _, command := range commands {
		found := -1
		for i, step := range job.Steps {
			if strings.Contains(step.Run, command) {
				if step.If != "" || step.ContinueOnError {
					return -1, fmt.Errorf("installed verification can skip %q", command)
				}
				found = i
				if strings.Contains(command, "console-installed.py run") && strings.Contains(step.Run, "--spec") {
					return -1, fmt.Errorf("release verification selects only part of the installed suite")
				}
				break
			}
		}
		if found <= last {
			return -1, fmt.Errorf("missing or out-of-order installed verification command %q", command)
		}
		last = found
	}
	return last, nil
}

func TestReleaseInstallationRejectsMissingFixtureWriterAuthorization(t *testing.T) {
	job := readReleaseWorkflow(t, "verification.yml").Jobs["installation"]
	steps := make([]releaseStep, 0, len(job.Steps))
	for _, step := range job.Steps {
		if !strings.Contains(step.Run, "admission.nativeWriters") {
			steps = append(steps, step)
		}
	}
	if len(steps) != len(job.Steps)-1 {
		t.Fatal("expected one fixture writer authorization step")
	}
	job.Steps = steps
	if _, err := validateInstalledVerificationSequence(job); err == nil || !strings.Contains(err.Error(), "helm upgrade orbitjob") {
		t.Fatalf("installation without fixture writer authorization was not rejected: %v", err)
	}
}

func TestInstalledFixtureWriterAuthorizationIsDedicatedAndPreservesInstallation(t *testing.T) {
	job := readReleaseWorkflow(t, "verification.yml").Jobs["installation"]
	var authorization releaseStep
	for _, step := range job.Steps {
		if strings.Contains(step.Run, "admission.nativeWriters") {
			if authorization.Run != "" {
				t.Fatal("multiple fixture writer authorization steps")
			}
			authorization = step
		}
	}
	if authorization.Run == "" || authorization.If != "" || authorization.ContinueOnError {
		t.Fatal("fixture writer authorization is missing or optional")
	}

	for _, tc := range []struct {
		name, cluster, context, username        string
		contextStatus, whoamiStatus, helmStatus int
		pass, wantHelm                          bool
	}{
		{name: "dedicated kind administrator", cluster: "orbitjob-console-e2e", context: "kind-orbitjob-console-e2e", username: "kubernetes-admin", pass: true, wantHelm: true},
		{name: "another cluster", cluster: "another-cluster", context: "kind-another-cluster", username: "kubernetes-admin"},
		{name: "another context", cluster: "orbitjob-console-e2e", context: "shared-cluster", username: "kubernetes-admin"},
		{name: "context lookup fails", cluster: "orbitjob-console-e2e", context: "kind-orbitjob-console-e2e", username: "kubernetes-admin", contextStatus: 1},
		{name: "another identity", cluster: "orbitjob-console-e2e", context: "kind-orbitjob-console-e2e", username: "application-user"},
		{name: "identity lookup fails", cluster: "orbitjob-console-e2e", context: "kind-orbitjob-console-e2e", username: "kubernetes-admin", whoamiStatus: 1},
		{name: "upgrade or hook fails", cluster: "orbitjob-console-e2e", context: "kind-orbitjob-console-e2e", username: "kubernetes-admin", helmStatus: 1, wantHelm: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			kubeconfig := filepath.Join(dir, "private-kubeconfig")
			arguments := filepath.Join(dir, "helm-arguments")
			for name, script := range map[string]string{
				"kubectl": `#!/bin/sh
test "$KUBECONFIG" = "$EXPECTED_KUBECONFIG" || exit 92
case "$*" in
  'config current-context')
    printf '%s\n' "$TEST_CONTEXT"
    exit "$CONTEXT_STATUS"
    ;;
  'auth whoami -o jsonpath={.status.userInfo.username}')
    printf '%s\n' "$TEST_USERNAME"
    exit "$WHOAMI_STATUS"
    ;;
  *) exit 93 ;;
esac
`,
				"helm": `#!/bin/sh
test "$KUBECONFIG" = "$EXPECTED_KUBECONFIG" || exit 94
printf '%s\n' "$@" > "$HELM_ARGUMENTS"
exit "$HELM_STATUS"
`,
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", authorization.Run)
			cmd.Env = append(gateEnvironment("PATH", "KUBECONFIG", "EXPECTED_KUBECONFIG", "KIND_CLUSTER_NAME", "TEST_CONTEXT", "TEST_USERNAME", "CONTEXT_STATUS", "WHOAMI_STATUS", "HELM_ARGUMENTS", "HELM_STATUS"),
				"PATH="+dir+":"+os.Getenv("PATH"), "KUBECONFIG="+kubeconfig, "EXPECTED_KUBECONFIG="+kubeconfig,
				"KIND_CLUSTER_NAME="+tc.cluster, "TEST_CONTEXT="+tc.context, "TEST_USERNAME="+tc.username,
				fmt.Sprintf("CONTEXT_STATUS=%d", tc.contextStatus), fmt.Sprintf("WHOAMI_STATUS=%d", tc.whoamiStatus), "HELM_ARGUMENTS="+arguments, fmt.Sprintf("HELM_STATUS=%d", tc.helmStatus))
			output, err := cmd.CombinedOutput()
			if (err == nil) != tc.pass {
				t.Fatalf("authorization pass=%v: %v\n%s", tc.pass, err, output)
			}
			data, readErr := os.ReadFile(arguments)
			if !tc.wantHelm {
				if !os.IsNotExist(readErr) {
					t.Fatalf("unsafe identity or context reached Helm: %v %s", readErr, data)
				}
				return
			}
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := []string{
				"upgrade", "orbitjob", "./charts/orbitjob", "-n", "orbitjob",
				"--kube-context", "kind-orbitjob-console-e2e", "--reuse-values",
				"--set-json", `admission.nativeWriters=["kubernetes-admin"]`,
				"--wait", "--wait-for-jobs", "--timeout", "8m",
			}
			if got := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"); !slices.Equal(got, want) {
				t.Fatalf("fixture authorization must retain installed values and change only the dedicated writer: got %q, want %q", got, want)
			}
		})
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

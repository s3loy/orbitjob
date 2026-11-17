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

type documentationWorkflow struct {
	On          map[string]yaml.Node        `yaml:"on"`
	Permissions map[string]string           `yaml:"permissions"`
	Concurrency yaml.Node                   `yaml:"concurrency"`
	Jobs        map[string]documentationJob `yaml:"jobs"`
}

type documentationJob struct {
	Uses            string            `yaml:"uses"`
	Needs           []string          `yaml:"needs"`
	With            map[string]string `yaml:"with"`
	If              string            `yaml:"if"`
	ContinueOnError bool              `yaml:"continue-on-error"`
	Permissions     map[string]string `yaml:"permissions"`
	Concurrency     yaml.Node         `yaml:"concurrency"`
	Outputs         map[string]string `yaml:"outputs"`
	Steps           []struct {
		releaseStep `yaml:",inline"`
		ID          string            `yaml:"id"`
		Env         map[string]string `yaml:"env"`
	} `yaml:"steps"`
}

func TestDocumentationIsMandatoryBeforeImagePublication(t *testing.T) {
	for _, name := range []string{"release.yml", "prerelease.yml"} {
		t.Run(name, func(t *testing.T) {
			workflow := readReleaseWorkflow(t, name)
			images := workflow.Jobs["images"]
			verify := workflow.Jobs["verify"]
			if !slices.Contains(images.Needs, "verify") || images.If != "" || images.ContinueOnError {
				t.Fatal("image publication can bypass a failed or skipped verification")
			}
			if verify.Uses != "./.github/workflows/verification.yml" || verify.If != "" || verify.ContinueOnError {
				t.Fatal("image publication verification is missing or conditional")
			}
			wantRef := "${{ github.sha }}"
			if name == "release.yml" {
				wantRef = "${{ needs.resolve.outputs.sha }}"
			}
			if verify.With["source_ref"] != wantRef {
				t.Fatalf("publication verifies %q, want %q", verify.With["source_ref"], wantRef)
			}
		})
	}

	workflow := readDocumentationWorkflow(t, "verification.yml")
	docs, ok := workflow.Jobs["documentation"]
	if !ok || docs.Uses != "./.github/workflows/docs-verification.yml" || docs.If != "" || docs.ContinueOnError {
		t.Fatal("release documentation verification is missing or optional")
	}
	if docs.With["source_ref"] != "${{ inputs.source_ref }}" || docs.With["build_artifact_name"] != "" || docs.With["include_benchmark_history"] != "" {
		t.Fatal("release documentation must verify only the selected source without publication artifacts or mutable benchmark history")
	}
	requireDocumentationReadOnly(t, docs.Permissions)
	installation := workflow.Jobs["installation"]
	if !slices.Contains(installation.Needs, "documentation") || installation.If != "" || installation.ContinueOnError {
		t.Fatal("installation can proceed after failed or skipped documentation verification")
	}
}

func TestReusableDocumentationVerifiesExactSourceWithoutPages(t *testing.T) {
	workflow := readDocumentationWorkflow(t, "docs-verification.yml")
	requireDocumentationReadOnly(t, workflow.Permissions)
	var call struct {
		Inputs map[string]struct {
			Required bool   `yaml:"required"`
			Type     string `yaml:"type"`
		} `yaml:"inputs"`
	}
	callNode := workflow.On["workflow_call"]
	if err := callNode.Decode(&call); err != nil {
		t.Fatal(err)
	}
	if len(workflow.On) != 1 || !call.Inputs["source_ref"].Required || call.Inputs["source_ref"].Type != "string" {
		t.Fatal("reusable documentation must require an explicit source revision")
	}
	if workflow.Concurrency.Kind != 0 {
		t.Fatal("reusable documentation concurrency can cancel its standalone caller")
	}
	if len(workflow.Jobs) != 1 {
		t.Fatal("reusable documentation has unexpected jobs")
	}
	job := workflow.Jobs["documentation"]
	if job.If != "" || job.ContinueOnError || job.Concurrency.Kind != 0 {
		t.Fatal("reusable documentation can skip workflow_call or cancel its caller")
	}
	checkouts := 0
	siteArtifacts := 0
	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, "actions/checkout@") {
			checkouts++
			if step.With["ref"] != "${{ inputs.source_ref }}" || step.With["persist-credentials"] != "false" || step.If != "" || step.ContinueOnError {
				t.Fatal("documentation checkout is not bound to the selected source")
			}
		}
		if strings.Contains(step.Uses, "pages") || strings.Contains(step.Uses, "deploy") || strings.Contains(step.Run, "gh api") || strings.Contains(step.Run, "git push") {
			t.Fatalf("reusable documentation has publication side effects: %+v", step)
		}
		if strings.HasPrefix(step.Uses, "actions/upload-artifact@") && step.With["path"] == "site/dist" {
			siteArtifacts++
			if step.If != "inputs.build_artifact_name != ''" || step.ContinueOnError || step.With["name"] != "${{ inputs.build_artifact_name }}" || step.With["include-hidden-files"] != "true" || step.With["if-no-files-found"] != "error" {
				t.Fatal("optional site artifact must preserve the complete successful build, including .nojekyll")
			}
		}
	}
	if checkouts != 1 {
		t.Fatalf("expected one exact-source checkout, got %d", checkouts)
	}
	if siteArtifacts != 1 {
		t.Fatalf("expected one optional verified site artifact, got %d", siteArtifacts)
	}
	for _, command := range []string{
		"npm --prefix site ci --no-audit --no-fund",
		"make openapi-check",
		"npm --prefix site run build",
		"npm --prefix site exec -- playwright install --with-deps chromium",
		"npm --prefix site run test:browser",
	} {
		requireWorkflowCommand(t, readReleaseWorkflow(t, "docs-verification.yml").Jobs["documentation"], command)
	}
}

func TestDocumentationSourceConfirmationRejectsMutableOrDifferentRefs(t *testing.T) {
	workflow := readDocumentationWorkflow(t, "docs-verification.yml")
	var confirmation string
	for _, step := range workflow.Jobs["documentation"].Steps {
		if strings.Contains(step.Run, "git rev-parse HEAD") && step.Env["SOURCE_REF"] == "${{ inputs.source_ref }}" && step.If == "" && !step.ContinueOnError {
			confirmation = step.Run
		}
	}
	if confirmation == "" {
		t.Fatal("documentation does not confirm the selected commit")
	}
	sha := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name, source, checkedOut string
		pass                     bool
	}{
		{"exact commit", sha, sha, true},
		{"different commit", sha, strings.Repeat("b", 40), false},
		{"moving branch", "main", "main", false},
		{"abbreviated commit", "aaaaaaa", "aaaaaaa", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeGateExecutable(t, filepath.Join(dir, "git"), "#!/bin/sh\nprintf '%s\\n' \"$CHECKED_OUT\"\n")
			cmd := exec.Command("bash", "-e", "-c", confirmation)
			cmd.Env = append(gateEnvironment("PATH", "SOURCE_REF", "CHECKED_OUT"), "PATH="+dir+":"+os.Getenv("PATH"), "SOURCE_REF="+tc.source, "CHECKED_OUT="+tc.checkedOut)
			output, err := cmd.CombinedOutput()
			if (err == nil) != tc.pass {
				t.Fatalf("source=%q checkout=%q pass=%v: %v %s", tc.source, tc.checkedOut, tc.pass, err, output)
			}
		})
	}
}

func TestStandaloneDocumentationRetainsTriggersAndOwnsPages(t *testing.T) {
	workflow := readDocumentationWorkflow(t, "docs.yml")
	for _, event := range []string{"push", "pull_request"} {
		var trigger struct {
			Branches []string `yaml:"branches"`
		}
		triggerNode := workflow.On[event]
		if err := triggerNode.Decode(&trigger); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(trigger.Branches, []string{"main", "dev"}) {
			t.Fatalf("standalone documentation lost %s branch coverage", event)
		}
	}
	var benchmark struct {
		Workflows []string `yaml:"workflows"`
		Branches  []string `yaml:"branches"`
		Types     []string `yaml:"types"`
	}
	benchmarkNode := workflow.On["workflow_run"]
	if err := benchmarkNode.Decode(&benchmark); err != nil {
		t.Fatal(err)
	}
	if _, ok := workflow.On["workflow_dispatch"]; !ok || !slices.Equal(benchmark.Workflows, []string{"Benchmark"}) || !slices.Equal(benchmark.Branches, []string{"main"}) || !slices.Equal(benchmark.Types, []string{"completed"}) {
		t.Fatal("standalone documentation lost manual or Benchmark coverage")
	}
	resolve := workflow.Jobs["resolve"]
	if resolve.If != "github.event_name != 'workflow_run' || github.event.workflow_run.conclusion == 'success'" || resolve.Outputs["sha"] != "${{ steps.source.outputs.sha }}" {
		t.Fatal("standalone documentation must resolve successful Benchmark refreshes to a commit")
	}
	resolved := false
	for _, step := range resolve.Steps {
		if strings.HasPrefix(step.Uses, "actions/checkout@") && step.With["ref"] != "${{ github.event_name == 'workflow_run' && 'main' || github.sha }}" {
			t.Fatal("standalone source selection no longer preserves current-main Benchmark refreshes")
		}
		if step.ID == "source" && strings.Contains(step.Run, "git rev-parse HEAD") && strings.Contains(step.Run, "GITHUB_OUTPUT") && step.If == "" && !step.ContinueOnError {
			resolved = true
		}
	}
	build := workflow.Jobs["build"]
	if !resolved || !slices.Contains(build.Needs, "resolve") || build.Uses != "./.github/workflows/docs-verification.yml" || build.If != "" || build.ContinueOnError || build.With["source_ref"] != "${{ needs.resolve.outputs.sha }}" || build.With["include_benchmark_history"] != "true" {
		t.Fatal("standalone documentation no longer uses mandatory shared verification at its resolved commit")
	}
	requireDocumentationReadOnly(t, build.Permissions)
	for id, predecessor := range map[string]string{"pages": "build", "deploy": "pages"} {
		job := workflow.Jobs[id]
		if job.If != "github.ref == 'refs/heads/main' && github.event_name != 'pull_request'" || !slices.Contains(job.Needs, predecessor) || job.ContinueOnError {
			t.Fatalf("%s no longer requires successful verification on the standalone main path", id)
		}
	}
	pages := workflow.Jobs["pages"]
	downloaded := false
	uploaded := false
	for _, step := range pages.Steps {
		if strings.HasPrefix(step.Uses, "actions/download-artifact@") && step.With["name"] == build.With["build_artifact_name"] && step.With["path"] == "site/dist" && step.If == "" && !step.ContinueOnError {
			downloaded = true
		}
		if strings.HasPrefix(step.Uses, "actions/upload-pages-artifact@") && downloaded && step.With["path"] == "site/dist" && step.If == "" && !step.ContinueOnError {
			uploaded = true
		}
	}
	if !uploaded {
		t.Fatal("Pages does not consume the verified documentation artifact")
	}
}

func requireDocumentationReadOnly(t *testing.T, permissions map[string]string) {
	t.Helper()
	if len(permissions) != 1 || permissions["contents"] != "read" {
		t.Fatalf("documentation verification permissions are not restricted to contents: read: %v", permissions)
	}
}

func readDocumentationWorkflow(t *testing.T, name string) documentationWorkflow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var workflow documentationWorkflow
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow
}

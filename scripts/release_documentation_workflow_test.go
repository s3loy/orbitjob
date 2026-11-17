package main

import (
	"maps"
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
	Steps           []struct {
		releaseStep `yaml:",inline"`
		ID          string            `yaml:"id"`
		Env         map[string]string `yaml:"env"`
	} `yaml:"steps"`
}

func TestReleaseDocumentationVerificationPolicy(t *testing.T) {
	for _, tc := range []struct{ workflow, source, verifyDocs string }{
		{"release.yml", "${{ needs.resolve.outputs.sha }}", "true"},
		{"prerelease.yml", "${{ github.sha }}", "false"},
	} {
		t.Run(tc.workflow, func(t *testing.T) {
			workflow := readReleaseWorkflow(t, tc.workflow)
			images := workflow.Jobs["images"]
			verify := workflow.Jobs["verify"]
			if !slices.Contains(images.Needs, "verify") || images.If != "" || images.ContinueOnError {
				t.Fatal("image publication can bypass a failed or skipped verification")
			}
			if verify.Uses != "./.github/workflows/verification.yml" || verify.If != "" || verify.ContinueOnError {
				t.Fatal("image publication verification is missing or conditional")
			}
			if verify.With["source_ref"] != tc.source || verify.With["verify_documentation"] != tc.verifyDocs {
				t.Fatalf("unexpected publication verification inputs: %v", verify.With)
			}
		})
	}

	workflow := readDocumentationWorkflow(t, "verification.yml")
	if input := readDocumentationCallInputs(t, workflow)["verify_documentation"]; input.Type != "boolean" || input.Default != true {
		t.Fatal("release verification must require documentation by default")
	}
	docs, ok := workflow.Jobs["documentation"]
	if !ok || docs.Uses != "./.github/workflows/docs-verification.yml" || docs.If != "inputs.verify_documentation" || docs.ContinueOnError {
		t.Fatal("release documentation must run when requested and propagate failure")
	}
	if docs.With["source_ref"] != "${{ inputs.source_ref }}" || len(docs.With) != 1 {
		t.Fatal("release documentation must verify only the selected source using the default build without publication artifacts or mutable benchmark history")
	}
	requireDocumentationReadOnly(t, docs.Permissions)
}

func TestInstallationPrerequisitesFailClosed(t *testing.T) {
	workflow := readDocumentationWorkflow(t, "verification.yml")
	installation := workflow.Jobs["installation"]
	gates := []string{"backend", "postgres", "vulnerabilities", "console", "documentation"}
	if !slices.Equal(installation.Needs, gates) || installation.If != "${{ !cancelled() }}" || installation.ContinueOnError {
		t.Fatal("installation must check all verification results once the prerequisites finish")
	}
	if len(installation.Steps) == 0 {
		t.Fatal("installation has no steps")
	}
	check := installation.Steps[0]
	if check.ID != "prerequisites" || check.If != "" || check.ContinueOnError {
		t.Fatal("installation must require successful prerequisites before checking out source")
	}
	wantEnv := map[string]string{"VERIFY_DOCUMENTATION": "${{ inputs.verify_documentation }}"}
	for _, gate := range gates {
		wantEnv[strings.ToUpper(gate)+"_RESULT"] = "${{ needs." + gate + ".result }}"
	}
	if !maps.Equal(check.Env, wantEnv) {
		t.Fatalf("incorrect inputs to installation prerequisite check: %v", check.Env)
	}

	run := func(t *testing.T, overrides map[string]string, pass bool) {
		t.Helper()
		results := map[string]string{"VERIFY_DOCUMENTATION": "true"}
		for _, gate := range gates {
			results[strings.ToUpper(gate)+"_RESULT"] = "success"
		}
		maps.Copy(results, overrides)
		keys := slices.Collect(maps.Keys(results))
		env := gateEnvironment(keys...)
		for key, value := range results {
			env = append(env, key+"="+value)
		}
		cmd := exec.Command("bash", "-e", "-c", check.Run)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if (err == nil) != pass {
			t.Fatalf("results=%v pass=%v: %v %s", results, pass, err, out)
		}
	}
	t.Run("all-success", func(t *testing.T) { run(t, nil, true) })
	t.Run("prerelease-no-documentation", func(t *testing.T) {
		run(t, map[string]string{"VERIFY_DOCUMENTATION": "false", "DOCUMENTATION_RESULT": "skipped"}, true)
	})
	for _, gate := range gates {
		for _, result := range []string{"skipped", "failure", "cancelled", ""} {
			t.Run(gate+"="+result, func(t *testing.T) {
				run(t, map[string]string{strings.ToUpper(gate) + "_RESULT": result}, false)
			})
		}
	}
	t.Run("prerelease-unexpected-documentation", func(t *testing.T) {
		run(t, map[string]string{"VERIFY_DOCUMENTATION": "false"}, false)
	})
	t.Run("prerelease-required-gate-skipped", func(t *testing.T) {
		run(t, map[string]string{"VERIFY_DOCUMENTATION": "false", "DOCUMENTATION_RESULT": "skipped", "CONSOLE_RESULT": "skipped"}, false)
	})
}

func TestReusableDocumentationVerifiesExactSourceWithoutPages(t *testing.T) {
	workflow := readDocumentationWorkflow(t, "docs-verification.yml")
	requireDocumentationReadOnly(t, workflow.Permissions)
	inputs := readDocumentationCallInputs(t, workflow)
	if len(workflow.On) != 1 || !inputs["source_ref"].Required || inputs["source_ref"].Type != "string" {
		t.Fatal("reusable documentation must require an explicit source revision")
	}
	if inputs["include_benchmark_history"].Type != "boolean" || inputs["include_benchmark_history"].Default != false || inputs["build_artifact_name"].Type != "string" || inputs["build_artifact_name"].Default != "" {
		t.Fatal("benchmark history and the publication artifact must be opt-in")
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
	history := false
	githubBuild := false
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
		if step.ID == "history" {
			history = step.If == "inputs.include_benchmark_history" && !step.ContinueOnError && strings.Contains(step.Run, "site/scripts/fetch-benchmark-history.sh")
		}
		if step.Run == "npm --prefix site run build" && step.If == "" && !step.ContinueOnError {
			githubBuild = step.Env["BENCHMARK_ARCHIVE"] == "${{ steps.history.outputs.archive }}"
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
	if !history {
		t.Fatal("standalone GitHub Pages verification must use the shared benchmark history fetcher when opted in")
	}
	if !githubBuild {
		t.Fatal("reusable documentation must build the site with any retrieved benchmark history")
	}
	for _, command := range []string{
		"npm --prefix site ci --no-audit --no-fund",
		"make openapi-check",
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

func TestManualDocumentationPublishesMainOnly(t *testing.T) {
	workflow := readDocumentationWorkflow(t, "docs.yml")
	requireDocumentationReadOnly(t, workflow.Permissions)
	if _, ok := workflow.On["workflow_dispatch"]; !ok || len(workflow.On) != 1 {
		t.Fatal("standalone documentation must run only on manual dispatch")
	}
	if len(workflow.Jobs) != 3 {
		t.Fatal("standalone documentation must run only the GitHub Pages publication path")
	}
	const mainOnly = "github.ref == 'refs/heads/main'"
	build := workflow.Jobs["build"]
	if len(build.Needs) != 0 || build.Uses != "./.github/workflows/docs-verification.yml" || build.If != mainOnly || build.ContinueOnError {
		t.Fatal("manual documentation must run shared verification on main")
	}
	requireDocumentationReadOnly(t, build.Permissions)
	if build.With["source_ref"] != "${{ github.sha }}" || build.With["include_benchmark_history"] != "true" || build.With["build_artifact_name"] != "documentation-site" {
		t.Fatal("manual documentation must build the dispatched commit with benchmark history")
	}
	for id, predecessor := range map[string]string{"pages": "build", "deploy": "pages"} {
		job := workflow.Jobs[id]
		if job.If != mainOnly || !slices.Equal(job.Needs, []string{predecessor}) || job.ContinueOnError {
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

type documentationCallInput struct {
	Required bool   `yaml:"required"`
	Type     string `yaml:"type"`
	Default  any    `yaml:"default"`
}

func readDocumentationCallInputs(t *testing.T, workflow documentationWorkflow) map[string]documentationCallInput {
	t.Helper()
	var call struct {
		Inputs map[string]documentationCallInput `yaml:"inputs"`
	}
	node := workflow.On["workflow_call"]
	if err := node.Decode(&call); err != nil {
		t.Fatal(err)
	}
	return call.Inputs
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

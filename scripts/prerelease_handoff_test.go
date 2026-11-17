package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPreReleaseHandoffUsesSelectedSourceAndPreparedInstallation(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/prerelease.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string
				Env map[string]string
			}
		}
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	var script string
	for _, step := range workflow.Jobs["publish"].Steps {
		if !strings.Contains(step.Run, "helm upgrade --install") {
			continue
		}
		if script != "" {
			t.Fatal("multiple installation handoffs need explicit verification")
		}
		if step.Env["VERSION"] != "${{ needs.images.outputs.version }}" || step.Env["SOURCE_SHA"] != "${{ github.sha }}" {
			t.Fatal("installation handoff must use this run's image tag and selected source")
		}
		script = step.Run
	}
	if script == "" {
		t.Fatal("pre-release installation handoff is missing")
	}

	dir := t.TempDir()
	summary := filepath.Join(dir, "summary.md")
	sha := strings.Repeat("c", 40)
	version := "rc-1234-ccccccc"
	cmd := exec.Command("bash", "-e", "-c", script)
	cmd.Env = append(gateEnvironment("GITHUB_REPOSITORY_OWNER", "GITHUB_REPOSITORY", "GITHUB_SERVER_URL", "GITHUB_STEP_SUMMARY", "SOURCE_SHA", "VERSION", "RELEASE_NAMESPACE", "VALUES_FILE"),
		"GITHUB_REPOSITORY_OWNER=Publication-Test", "GITHUB_REPOSITORY=Publication-Test/orbitjob",
		"GITHUB_SERVER_URL=https://github.example.com", "GITHUB_STEP_SUMMARY="+summary, "SOURCE_SHA="+sha, "VERSION="+version)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("print handoff: %v\n%s", err, output)
	}
	publishedSummary, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output, publishedSummary) {
		t.Fatal("workflow log and run summary present different installation handoffs")
	}
	for _, guide := range []string{"docs/setup/installation.md", "docs/operations/upgrading.md"} {
		if _, err := os.Stat(filepath.Join("..", guide)); err != nil {
			t.Fatal(err)
		}
		link := "https://github.example.com/Publication-Test/orbitjob/blob/" + sha + "/" + guide
		if !strings.Contains(string(output), link) {
			t.Fatalf("handoff does not link the selected source guide %s", guide)
		}
	}
	for _, prerequisite := range []string{"release and execution namespaces", "PostgreSQL", "database Secret", "values file", "node can pull all seven images", "Component-specific tags", "component repository overrides"} {
		if !strings.Contains(string(output), prerequisite) {
			t.Errorf("handoff omits prerequisite %q", prerequisite)
		}
	}
	if strings.Contains(string(output), "Install on any cluster") {
		t.Fatal("handoff advertises an installation without prerequisites")
	}

	fenced := regexp.MustCompile("(?s)```bash\\n(.*?)\\n```").FindSubmatch(output)
	if len(fenced) != 2 {
		t.Fatal("handoff does not provide an executable installation example")
	}
	command := string(fenced[1])
	if !strings.Contains(command, "$RELEASE_NAMESPACE") || !strings.Contains(command, "$VALUES_FILE") {
		t.Fatal("installation example substitutes an unreviewed namespace or values file")
	}

	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("Helm is required to render the published installation example")
	}
	values := filepath.Join(dir, "reviewed values.yaml")
	if err := os.WriteFile(values, []byte(`global:
  imageRegistry: registry.example.invalid/stale-values
  imageTag: stale-values-tag
operator:
  namespaceTenants: reviewed-tasks=00000000000000000000000001
  image: {tag: retained-old-tag}
images:
  admin: {tag: retained-old-tag}
  scheduler: {tag: retained-old-tag}
  migrate: {tag: retained-old-tag}
  bootstrap: {tag: retained-old-tag}
logs:
  image: {tag: retained-old-tag}
admission:
  image: {tag: retained-old-tag}
taskNamespace:
  create: false
  name: reviewed-tasks
database:
  secretName: reviewed-database
`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Replace only the cluster mutation with local rendering; execute the
	// printed example unchanged so quoting and all supplied inputs are covered.
	renderScript := `set -eu
helm() {
  test "$1" = upgrade && test "$2" = --install
  shift 2
  command "$HANDOFF_HELM" template "$@"
}
` + command
	render := exec.Command("bash", "-c", renderScript)
	render.Dir = ".."
	render.Env = append(gateEnvironment("HANDOFF_HELM", "RELEASE_NAMESPACE", "VALUES_FILE"),
		"HANDOFF_HELM="+helm, "RELEASE_NAMESPACE=reviewed-control-plane", "VALUES_FILE="+values)
	manifests, err := render.CombinedOutput()
	if err != nil {
		t.Fatalf("render printed example with reviewed values: %v\n%s", err, manifests)
	}
	assertPreReleaseHandoffRender(t, manifests, "ghcr.io/publication-test/orbitjob-", version)

	globalOnly, err := exec.Command(helm, "template", "orbitjob", "../charts/orbitjob", "-f", values,
		"--set-string", "global.imageRegistry=ghcr.io/publication-test", "--set-string", "global.imageTag="+version).CombinedOutput()
	if err != nil || !strings.Contains(string(globalOnly), "ghcr.io/publication-test/orbitjob-admin-api:retained-old-tag") {
		t.Fatalf("global-only override must demonstrate the retained component tag: %v\n%s", err, globalOnly)
	}

	legacy, err := exec.Command(helm, "template", "orbitjob", "../charts/orbitjob", "--set", "global.imageTag="+version).CombinedOutput()
	if err == nil || !strings.Contains(string(legacy), "operator.namespaceTenants is required") {
		t.Fatalf("the old tag-only example must reject missing reviewed values: %v\n%s", err, legacy)
	}
}

func assertPreReleaseHandoffRender(t *testing.T, manifests []byte, imagePrefix, version string) {
	t.Helper()
	images := map[string]bool{}
	mapping, database, releaseNamespace := false, false, false
	var inspect func(any)
	inspect = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			if image, ok := value["image"].(string); ok && strings.Contains(image, "/orbitjob-") {
				if !strings.HasPrefix(image, imagePrefix) || !strings.HasSuffix(image, ":"+version) {
					t.Errorf("rendered component uses an image outside this run: %s", image)
				}
				images[image] = true
			}
			if value["name"] == "OPERATOR_NAMESPACE_TENANTS" && value["value"] == "reviewed-tasks=00000000000000000000000001" {
				mapping = true
			}
			if secret, ok := value["secretKeyRef"].(map[string]any); ok && secret["name"] == "reviewed-database" {
				database = true
			}
			if value["kind"] == "Deployment" {
				metadata, _ := value["metadata"].(map[string]any)
				if namespace := metadata["namespace"]; namespace != nil && namespace != "reviewed-control-plane" {
					t.Errorf("rendered Deployment ignores the prepared release namespace: %v", metadata)
				}
				if metadata["namespace"] == "reviewed-control-plane" {
					releaseNamespace = true
				}
			}
			for _, child := range value {
				inspect(child)
			}
		case []any:
			for _, child := range value {
				inspect(child)
			}
		}
	}
	decoder := yaml.NewDecoder(bytes.NewReader(manifests))
	for {
		var document any
		if err := decoder.Decode(&document); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		inspect(document)
	}
	if len(images) != 7 || !mapping || !database || !releaseNamespace {
		t.Fatalf("printed example lost prepared installation inputs: images=%d mapping=%v database=%v namespace=%v", len(images), mapping, database, releaseNamespace)
	}
}

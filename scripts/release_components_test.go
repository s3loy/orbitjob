package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPublishedComponentsCoverInstallation(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("Helm is required by the Kubernetes quality gate")
	}
	render, err := exec.Command(helm, "template", "publication", "../charts/orbitjob", "--namespace", "publication-test",
		"--set", "operator.namespaceTenants=tasks=00000000000000000000000001", "--set", "global.imageTag=rc-test").CombinedOutput()
	if err != nil {
		t.Fatalf("render installation: %v\n%s", err, render)
	}
	want := map[string]bool{}
	decoder := yaml.NewDecoder(bytes.NewReader(render))
	for {
		var document any
		if err := decoder.Decode(&document); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		installationComponents(document, want)
	}
	if len(want) == 0 {
		t.Fatal("rendered no OrbitJob component images")
	}
	dockerfile, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"release.yml", "prerelease.yml"} {
		t.Run(name, func(t *testing.T) {
			components, _ := publicationInputs(t, name)
			for component := range want {
				if components[component] == "" {
					t.Errorf("rendered installation requires unpublished component %s", component)
				}
			}
			for component, target := range components {
				stage := regexp.MustCompile(`(?im)^FROM\s+[^\n]+\s+AS\s+` + regexp.QuoteMeta(target) + `\s*$`)
				if !stage.Match(dockerfile) {
					t.Errorf("component %s names absent Docker build target %q", component, target)
				}
			}
		})
	}
}

func installationComponents(value any, components map[string]bool) {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if image, ok := child.(string); key == "image" && ok {
				_, component, found := strings.Cut(image, "/orbitjob-")
				if found {
					component, _, _ = strings.Cut(component, ":")
					components[component] = true
				}
			}
			installationComponents(child, components)
		}
	case []any:
		for _, child := range value {
			installationComponents(child, components)
		}
	}
}

func publicationInputs(t *testing.T, name string) (map[string]string, []releaseStep) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []struct{ Component, Target string }
				}
			}
			Steps []releaseStep
		}
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	components := map[string]string{}
	for _, item := range workflow.Jobs["images"].Strategy.Matrix.Include {
		if item.Component == "" || item.Target == "" || components[item.Component] != "" {
			t.Fatalf("invalid or duplicate publication component: %+v", item)
		}
		components[item.Component] = item.Target
	}
	return components, workflow.Jobs["publish"].Steps
}

func TestPreReleaseAliasesCoverPublishedComponents(t *testing.T) {
	components, steps := publicationInputs(t, "prerelease.yml")
	var command string
	for _, step := range steps {
		if strings.Contains(step.Run, "imagetools create") {
			if command != "" {
				t.Fatal("multiple alias operations need explicit verification")
			}
			command = step.Run
		}
	}
	if command == "" {
		t.Fatal("pre-release alias operation is missing")
	}
	directory := t.TempDir()
	log := filepath.Join(directory, "images")
	if err := os.WriteFile(filepath.Join(directory, "docker"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$AUDIT_IMAGE_LOG\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-e", "-c", command)
	cmd.Env = append(gateEnvironment("PATH", "REGISTRY", "GITHUB_REPOSITORY_OWNER", "VERSION", "AUDIT_IMAGE_LOG"),
		"PATH="+directory+string(os.PathListSeparator)+os.Getenv("PATH"), "REGISTRY=ghcr.io",
		"GITHUB_REPOSITORY_OWNER=Publication-Test", "VERSION=rc-test", "AUDIT_IMAGE_LOG="+log)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("alias operation: %v\n%s", err, output)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	var want []string
	for component := range components {
		image := "ghcr.io/publication-test/orbitjob-" + component
		want = append(want, "buildx imagetools create -t "+image+":rc "+image+":rc-test")
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("aliases differ from the published set\ngot: %q\nwant: %q", got, want)
	}
}

package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLocalManifestNamespaceOverrides(t *testing.T) {
	for _, kind := range []string{"kind-db", "load-operations"} {
		t.Run(kind, func(t *testing.T) {
			out, err := exec.Command("bash", "../render-local-manifest.sh", kind, "quality-custom").CombinedOutput()
			if err != nil {
				t.Fatalf("render: %v: %s", err, out)
			}
			decoder := yaml.NewDecoder(bytes.NewReader(out))
			for {
				var object struct {
					Kind     string `yaml:"kind"`
					Metadata struct {
						Name      string `yaml:"name"`
						Namespace string `yaml:"namespace"`
					} `yaml:"metadata"`
					Subjects []struct {
						Namespace string `yaml:"namespace"`
					} `yaml:"subjects"`
				}
				if err := decoder.Decode(&object); err != nil {
					if err == io.EOF {
						break
					}
					t.Fatal(err)
				}
				if kind == "kind-db" {
					if object.Kind == "Namespace" && object.Metadata.Name != "quality-custom" {
						t.Fatalf("Namespace name: %q", object.Metadata.Name)
					}
					if object.Kind != "Namespace" && object.Metadata.Namespace != "quality-custom" {
						t.Fatalf("%s namespace: %q", object.Kind, object.Metadata.Namespace)
					}
				} else {
					want := "quality-custom"
					if object.Kind == "ServiceAccount" {
						want = TaskNamespace
					}
					if object.Metadata.Namespace != want {
						t.Fatalf("%s namespace: %q, want %q", object.Kind, object.Metadata.Namespace, want)
					}
					for _, subject := range object.Subjects {
						if subject.Namespace != TaskNamespace {
							t.Fatalf("task principal moved to %q", subject.Namespace)
						}
					}
				}
			}
		})
	}
	for _, invalid := range []string{"", "UPPER", "bad/namespace", strings.Repeat("a", 64)} {
		if err := exec.Command("bash", "../render-local-manifest.sh", "kind-db", invalid).Run(); err == nil {
			t.Fatalf("invalid namespace accepted: %q", invalid)
		}
	}
}

func TestApplyOperationsRBACUsesResolvedNamespace(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "applied.yaml")
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte("#!/bin/sh\ncat > \"$QUALITY_APPLIED_MANIFEST\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("QUALITY_APPLIED_MANIFEST", capture)
	t.Setenv("ORBITJOB_NAMESPACE", "quality-custom")
	if err := kubectlApply("../../deploy/load/operations-rbac.yaml"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("namespace: quality-custom")) || bytes.Contains(data, []byte("namespace: orbitjob-system")) {
		t.Fatalf("applied RBAC ignored namespace override: %s", data)
	}
}

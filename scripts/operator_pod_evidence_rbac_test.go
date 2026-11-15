package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestOperatorPodEvidencePatchIsNamespaceScoped(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required by the separate Kubernetes quality gate")
	}
	values := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(values, []byte("operator:\n  namespaceTenants: 'tasks-a=01ARZ3NDEKTSV4RRFFQ69G5FAV,tasks-b=01BBZ3NDEKTSV4RRFFQ69G5FAV'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("helm", "template", "evidence-test", "../charts/orbitjob", "--namespace", "install", "-f", values)
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("render chart: %v\n%s", err, data)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	patched := map[string]bool{}
	for {
		var role rbacv1.Role
		if err = decoder.Decode(&role); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if role.Kind != "Role" && role.Kind != "ClusterRole" {
			continue
		}
		for _, rule := range role.Rules {
			if !slices.Contains(rule.Resources, "pods") {
				continue
			}
			mutates := false
			for _, verb := range []string{"create", "update", "patch", "delete", "deletecollection", "*"} {
				mutates = mutates || slices.Contains(rule.Verbs, verb)
			}
			if !mutates {
				continue
			}
			if role.Kind != "Role" || role.Name != "orbitjob-operator" ||
				(role.Namespace != "tasks-a" && role.Namespace != "tasks-b") {
				t.Fatalf("Pod mutation escaped managed Operator roles: %s %s/%s", role.Kind, role.Namespace, role.Name)
			}
			if !slices.Equal(rule.APIGroups, []string{""}) || !slices.Equal(rule.Resources, []string{"pods"}) ||
				!slices.Equal(rule.Verbs, []string{"get", "list", "watch", "patch"}) {
				t.Fatalf("Operator gained broader Pod mutation: %+v", rule)
			}
			patched[role.Namespace] = true
		}
	}
	if len(patched) != 2 {
		t.Fatalf("Pod evidence patch missing a managed namespace: %v", patched)
	}
}

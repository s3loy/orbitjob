package main

import (
	"os/exec"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefinitionLogChartGrantsStayNamespacedAndExact(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm unavailable")
	}
	out, err := exec.Command("helm", "template", "orbitjob", "../../charts/orbitjob", "--set", "operator.namespaceTenants=own=00000000000000000000000001").CombinedOutput()
	if err != nil {
		t.Fatalf("helm render: %s", out)
	}
	type rule struct {
		APIGroups []string `yaml:"apiGroups"`
		Resources []string `yaml:"resources"`
		Verbs     []string `yaml:"verbs"`
	}
	type document struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			Name      string `yaml:"name"`
			Namespace string `yaml:"namespace"`
		} `yaml:"metadata"`
		Rules []rule `yaml:"rules"`
	}
	expected := map[string]string{"workloads.orbitjob.io/scheduledjobs": "get,create,patch", "workloads.orbitjob.io/workflowjobs": "get,create,patch", "workloads.orbitjob.io/jobruns": "create,get,patch", "workloads.orbitjob.io/workflowruns": "create,get,patch", "batch/jobs": "get", "/pods": "get,list", "/pods/log": "get"}
	seen := map[string]bool{}
	for _, raw := range strings.Split(string(out), "\n---") {
		var doc document
		if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatal(err)
		}
		if doc.Metadata.Name != "orbitjob-admin-api" {
			continue
		}
		if doc.Kind == "ClusterRole" {
			t.Fatal("admin received ClusterRole")
		}
		if doc.Kind != "Role" {
			continue
		}
		if doc.Metadata.Namespace != "own" {
			t.Fatalf("unexpected namespace: %s", doc.Metadata.Namespace)
		}
		for _, rule := range doc.Rules {
			for _, group := range rule.APIGroups {
				for _, res := range rule.Resources {
					key := group + "/" + res
					want, ok := expected[key]
					if !ok || want != strings.Join(rule.Verbs, ",") {
						t.Fatalf("unexpected grant %s: %v", key, rule.Verbs)
					}
					seen[key] = true
				}
			}
		}
	}
	if len(seen) != len(expected) {
		t.Fatalf("missing bounded grants: %+v", seen)
	}
	if !strings.Contains(string(out), "name: OPERATOR_NAMESPACE_TENANTS") {
		t.Fatal("Admin missing authoritative mapping")
	}
}

package main

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestQualificationLoadManifests(t *testing.T) {
	if err := ValidateLoadManifests("../../deploy/load/namespace.yaml", "../../deploy/load/operations-rbac.yaml"); err != nil {
		t.Fatal(err)
	}
}

func TestTenantNamespaceManifestGrantsOperatorInTenantNamespace(t *testing.T) {
	manifest := tenantNamespaceManifest("orbitjob-tasks-tenant-a", "orbitjob-system")
	if strings.Contains(manifest, "ClusterRole") {
		t.Fatal("tenant namespace manifest must not grant cluster-wide access")
	}

	type rule struct {
		APIGroups []string `yaml:"apiGroups"`
		Resources []string `yaml:"resources"`
		Verbs     []string `yaml:"verbs"`
	}
	type subject struct {
		Kind      string `yaml:"kind"`
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	}
	type object struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			Name      string `yaml:"name"`
			Namespace string `yaml:"namespace"`
		} `yaml:"metadata"`
		Rules    []rule    `yaml:"rules"`
		RoleRef  subject   `yaml:"roleRef"`
		Subjects []subject `yaml:"subjects"`
	}

	var role, binding object
	documents := 0
	decoder := yaml.NewDecoder(strings.NewReader(manifest))
	for {
		var got object
		if err := decoder.Decode(&got); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode tenant namespace manifest: %v", err)
		}
		documents++
		switch got.Kind {
		case "Role":
			role = got
		case "RoleBinding":
			binding = got
		}
	}
	if documents != 5 {
		t.Fatalf("manifest documents = %d, want 5", documents)
	}
	if role.Metadata.Name != "orbitjob-operator" || role.Metadata.Namespace != "orbitjob-tasks-tenant-a" {
		t.Fatalf("operator Role metadata = %s/%s", role.Metadata.Namespace, role.Metadata.Name)
	}
	wantRules := []rule{
		{[]string{"workloads.orbitjob.io"}, []string{"scheduledjobs"}, []string{"get", "list", "watch", "update", "patch"}},
		{[]string{"workloads.orbitjob.io"}, []string{"jobruns"}, []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{[]string{"workloads.orbitjob.io"}, []string{"workflowjobs", "workflowruns"}, []string{"get", "list", "watch", "patch"}},
		{[]string{"workloads.orbitjob.io"}, []string{"scheduledjobs/status", "jobruns/status", "workflowruns/status", "workflowjobs/status"}, []string{"get", "update", "patch"}},
		{[]string{"batch"}, []string{"jobs"}, []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{[]string{""}, []string{"pods"}, []string{"get", "list", "watch"}},
	}
	if !reflect.DeepEqual(role.Rules, wantRules) {
		t.Fatalf("operator Role rules = %#v, want %#v", role.Rules, wantRules)
	}
	wantRef := subject{Kind: "Role", Name: "orbitjob-operator"}
	wantSubjects := []subject{{Kind: "ServiceAccount", Name: "orbitjob-operator", Namespace: "orbitjob-system"}}
	if binding.Metadata.Namespace != "orbitjob-tasks-tenant-a" || !reflect.DeepEqual(binding.RoleRef, wantRef) || !reflect.DeepEqual(binding.Subjects, wantSubjects) {
		t.Fatalf("operator RoleBinding = %#v", binding)
	}
}

func TestWithBackoffRetriesOnRateLimit(t *testing.T) {
	attempts := 0
	err := withBackoff(context.Background(), func() error {
		attempts++
		if attempts < 3 {
			return &APIError{StatusCode: 429, Message: "rate limited"}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestWithBackoffDoesNotRetryClientError(t *testing.T) {
	attempts := 0
	err := withBackoff(context.Background(), func() error {
		attempts++
		return &APIError{StatusCode: 400, Message: "bad request"}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry on 400)", attempts)
	}
}

func TestWithBackoffGivesUpAfterCap(t *testing.T) {
	attempts := 0
	err := withBackoff(context.Background(), func() error {
		attempts++
		return &APIError{StatusCode: 0, Message: "connection refused"}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 5 {
		t.Fatalf("attempts = %d, want 5 (initial + 4 retries)", attempts)
	}
}

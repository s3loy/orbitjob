package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestAdmissionWebhookMatchesOnlyMappedNamespaces(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required by the Kubernetes quality gate")
	}
	for _, tc := range []struct {
		name, mapping string
		namespaces    []string
	}{
		{"first installation", " finance = 01ARZ3NDEKTSV4RRFFQ69G5FAV , reports = 01ARZ3NDEKTSV4RRFFQ69G5FAW ", []string{"finance", "reports"}},
		{"disjoint installation", "other-tasks=01ARZ3NDEKTSV4RRFFQ69G5FAX", []string{"other-tasks"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values, err := json.Marshal(map[string]any{"operator": map[string]string{"namespaceTenants": tc.mapping}})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "values.json")
			if err := os.WriteFile(path, values, 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("helm", "template", "namespace-test", "../charts/orbitjob", "--namespace", "install-system", "-f", path).CombinedOutput()
			if err != nil {
				t.Fatalf("Helm render: %v\n%s", err, out)
			}
			decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(out), 4096)
			var configuration *admissionv1.ValidatingWebhookConfiguration
			for {
				var raw json.RawMessage
				if err := decoder.Decode(&raw); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				var typ metav1.TypeMeta
				if err := json.Unmarshal(raw, &typ); err != nil {
					t.Fatal(err)
				}
				if typ.Kind != "ValidatingWebhookConfiguration" {
					continue
				}
				if configuration != nil {
					t.Fatal("duplicate validating webhook configuration")
				}
				configuration = &admissionv1.ValidatingWebhookConfiguration{}
				if err := json.Unmarshal(raw, configuration); err != nil {
					t.Fatal(err)
				}
			}
			if configuration == nil || len(configuration.Webhooks) != 1 {
				t.Fatalf("webhook configuration = %+v", configuration)
			}
			webhook := configuration.Webhooks[0]
			if webhook.FailurePolicy == nil || *webhook.FailurePolicy != admissionv1.Fail {
				t.Fatalf("managed namespaces must fail closed: %+v", webhook.FailurePolicy)
			}
			want := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key: "kubernetes.io/metadata.name", Operator: metav1.LabelSelectorOpIn, Values: tc.namespaces,
			}}}
			if !reflect.DeepEqual(webhook.NamespaceSelector, want) {
				t.Fatalf("namespace selector = %+v, want %+v", webhook.NamespaceSelector, want)
			}
			selector, err := metav1.LabelSelectorAsSelector(webhook.NamespaceSelector)
			if err != nil {
				t.Fatal(err)
			}
			for _, ns := range []string{"finance", "reports", "other-tasks", "unmanaged", "install-system"} {
				wantMatch := false
				for _, mapped := range tc.namespaces {
					if ns == mapped {
						wantMatch = true
					}
				}
				if got := selector.Matches(labels.Set{"kubernetes.io/metadata.name": ns}); got != wantMatch {
					t.Fatalf("namespace %q match=%v, want %v", ns, got, wantMatch)
				}
			}
			if webhook.ObjectSelector != nil {
				t.Fatalf("workload labels must not control admission: %+v", webhook.ObjectSelector)
			}
			if len(webhook.Rules) != 1 || !reflect.DeepEqual(webhook.Rules[0].Operations, []admissionv1.OperationType{admissionv1.Create, admissionv1.Update}) ||
				!reflect.DeepEqual(webhook.Rules[0].Resources, []string{"scheduledjobs", "workflowjobs"}) || webhook.Rules[0].Scope == nil || *webhook.Rules[0].Scope != admissionv1.NamespacedScope {
				t.Fatalf("definition admission rule changed: %+v", webhook.Rules)
			}
		})
	}
}

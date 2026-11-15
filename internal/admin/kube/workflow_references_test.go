package kube

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	"orbitjob/internal/admin/definition"
	"orbitjob/internal/domain/validation"
)

func TestWorkflowApplyChecksSameNamespaceScheduledJobsBeforeWrite(t *testing.T) {
	const spec = `{"tasks":[{"name":"extract","jobRef":{"name":"extract-job"}}]}`
	for _, kind := range []string{"missing", "foreign-namespace", "Function", "ScheduledJob"} {
		t.Run(kind, func(t *testing.T) {
			objects := []runtime.Object{}
			if kind != "missing" {
				ns, k := "own", kind
				if kind == "foreign-namespace" {
					ns = "other"
					k = "ScheduledJob"
				}
				objects = append(objects, &unstructured.Unstructured{Object: map[string]any{"apiVersion": "workloads.orbitjob.io/v1alpha1", "kind": k, "metadata": map[string]any{"name": "extract-job", "namespace": ns}, "spec": map[string]any{"jobTemplate": map[string]any{"image": "busybox"}}}})
			}
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
			d := DefinitionClient{Client: client, NamespaceTenants: map[string]string{"own": "tenant"}}
			_, digest, _ := definition.CanonicalSpec("workflows", spec)
			for _, dry := range []bool{true, false} {
				_, err := d.Apply(context.Background(), "workflows", "own", "pipeline", definition.ApplyRequest{RequestID: definitionTestRequest, SpecJSON: spec, DryRun: dry, ManifestDigest: digest})
				if kind == "ScheduledJob" {
					if err != nil {
						t.Fatal(err)
					}
					continue
				}
				var invalid *validation.Error
				if !errors.As(err, &invalid) || invalid.Field != "spec.tasks[0].jobRef.name" {
					t.Fatalf("unexpected missing reference error: %v", err)
				}
			}
			if kind != "ScheduledJob" {
				for _, action := range client.Actions() {
					if action.GetVerb() == "create" || action.GetVerb() == "patch" {
						t.Fatal("wrote invalid workflow")
					}
				}
			}
		})
	}
}

func TestWorkflowConfirmedReplayDoesNotDependOnLaterReferenceAvailability(t *testing.T) {
	job := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "workloads.orbitjob.io/v1alpha1", "kind": "ScheduledJob", "metadata": map[string]any{"name": "extract-job", "namespace": "own"}, "spec": map[string]any{"jobTemplate": map[string]any{"image": "busybox"}}}}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), job)
	d := DefinitionClient{Client: client, NamespaceTenants: map[string]string{"own": "tenant"}}
	const spec = `{"tasks":[{"name":"extract","jobRef":{"name":"extract-job"}}]}`
	canonical, digest, err := definition.CanonicalSpec("workflows", spec)
	if err != nil {
		t.Fatal(err)
	}
	input := definition.ApplyRequest{RequestID: definitionTestRequest, SpecJSON: canonical, ManifestDigest: digest}
	if _, err := d.Apply(context.Background(), "workflows", "own", "pipeline", input); err != nil {
		t.Fatal(err)
	}
	gvr, _, _ := definitionResource("jobs")
	if err := client.Resource(gvr).Namespace("own").Delete(context.Background(), "extract-job", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	replay, err := d.Apply(context.Background(), "workflows", "own", "pipeline", input)
	if err != nil || replay.State != "reconciled" {
		t.Fatalf("confirmed replay incorrectly re-admitted: %+v %v", replay, err)
	}
	input.RequestID = "b2098db5-f383-4fd7-b46d-61286a7f3869"
	if _, err := d.Apply(context.Background(), "workflows", "own", "pipeline", input); err == nil {
		t.Fatal("new write bypassed missing-reference check")
	}
}

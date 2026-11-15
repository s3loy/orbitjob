package kube

import (
	"context"
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

func jobRunFor(namespace, name string) v1alpha1.JobRun {
	return v1alpha1.JobRun{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha1.JobRunSpec{
			ScheduledJobRef:    v1alpha1.ObjectReference{Name: "nightly", UID: "def-1"},
			DefinitionRevision: 7,
			Trigger:            v1alpha1.Manual,
			Actor:              "alice",
			OccurrenceKey:      "occ-1",
		},
	}
}

func seededJobRun(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "workloads.orbitjob.io/v1alpha1",
		"kind":       "JobRun",
		"metadata":   map[string]any{"name": name, "namespace": namespace, "uid": "seeded-uid", "resourceVersion": "1"},
		"spec": map[string]any{
			"scheduledJobRef":    map[string]any{"name": "nightly", "uid": "def-1"},
			"definitionRevision": int64(7),
			"trigger":            string(v1alpha1.Manual),
			"actor":              "original",
			"occurrenceKey":      "occ-1",
			"cancelRequested":    false,
		},
	}}
}

// ---------------------------------------------------------------------------
// Publish
// ---------------------------------------------------------------------------

func TestPublishRequiresDynamicClient(t *testing.T) {
	publisher := JobRunPublisher{}
	_, _, err := publisher.Publish(context.Background(), jobRunFor("finance", "nightly-1"))
	if err == nil || !strings.Contains(err.Error(), "dynamic client is required") {
		t.Fatalf("expected missing-client error, got %v", err)
	}
}

func TestPublishRequiresNamespaceAndName(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	cases := []struct {
		name      string
		namespace string
		object    string
	}{
		{name: "missing namespace", namespace: "", object: "nightly-1"},
		{name: "missing name", namespace: "finance", object: ""},
	}
	for _, tc := range cases {
		_, _, err := (JobRunPublisher{Client: client}).Publish(
			context.Background(), jobRunFor(tc.namespace, tc.object))
		if err == nil || !strings.Contains(err.Error(), "job run namespace and name are required") {
			t.Fatalf("%s: expected identity error, got %v", tc.name, err)
		}
	}
}

func TestPublishCreatesTheJobRun(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())

	created, isNew, err := (JobRunPublisher{Client: client}).Publish(
		context.Background(), jobRunFor("finance", "nightly-abc12345"))
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if !isNew {
		t.Fatal("first publish must report that it created the object")
	}
	if created.Spec.Actor != "alice" || created.Spec.OccurrenceKey != "occ-1" {
		t.Fatalf("returned spec = %+v", created.Spec)
	}

	stored, err := client.Resource(jobRunGVR).Namespace("finance").
		Get(context.Background(), "nightly-abc12345", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("stored job run not found: %v", err)
	}
	actor, _, err := unstructured.NestedString(stored.Object, "spec", "actor")
	if err != nil || actor != "alice" {
		t.Fatalf("stored spec actor = %q (err %v), want alice", actor, err)
	}
}

func TestPublishReplayAdoptsTheExistingObject(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), seededJobRun("finance", "nightly-abc12345"))

	// The name is derived from the occurrence key, so replaying the same
	// trigger is deduplication, not a conflict.
	wanted := jobRunFor("finance", "nightly-abc12345")
	wanted.Spec.Actor = "original"
	run, isNew, err := (JobRunPublisher{Client: client}).Publish(context.Background(), wanted)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if isNew {
		t.Fatal("a replay must report that it did not create the object")
	}
	if run.Spec.Actor != "original" {
		t.Fatalf("replay returned actor %q, want the existing object's actor", run.Spec.Actor)
	}
}

func TestPublishWrapsReadFailureAfterAlreadyExists(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), seededJobRun("finance", "nightly-abc12345"))
	client.PrependReactor("get", "jobruns", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewInternalError(errors.New("api server unavailable"))
	})

	_, _, err := (JobRunPublisher{Client: client}).Publish(
		context.Background(), jobRunFor("finance", "nightly-abc12345"))
	if err == nil || !strings.Contains(err.Error(), "resolve existing run") {
		t.Fatalf("expected wrapped read error, got %v", err)
	}
}

func TestPublishWrapsCreateFailure(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("create", "jobruns", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("quota exceeded")
	})

	_, _, err := (JobRunPublisher{Client: client}).Publish(
		context.Background(), jobRunFor("finance", "nightly-abc12345"))
	if err == nil || !strings.Contains(err.Error(), "publish job run finance/nightly-abc12345") {
		t.Fatalf("expected wrapped create error, got %v", err)
	}
}

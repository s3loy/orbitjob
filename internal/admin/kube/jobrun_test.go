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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/domain/resource"
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
		"metadata":   map[string]any{"name": name, "namespace": namespace},
		"spec": map[string]any{
			"scheduledJobRef":    map[string]any{"name": "nightly", "uid": "def-1"},
			"definitionRevision": float64(7),
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
	run, isNew, err := (JobRunPublisher{Client: client}).Publish(
		context.Background(), jobRunFor("finance", "nightly-abc12345"))
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
	if err == nil || !strings.Contains(err.Error(), "read existing job run finance/nightly-abc12345") {
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
	if err == nil || !strings.Contains(err.Error(), "create job run finance/nightly-abc12345") {
		t.Fatalf("expected wrapped create error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// RequestCancel
// ---------------------------------------------------------------------------

func TestRequestCancelRequiresDynamicClient(t *testing.T) {
	publisher := JobRunPublisher{}
	err := publisher.RequestCancel(context.Background(), "finance", "nightly-1")
	if err == nil || !strings.Contains(err.Error(), "dynamic client is required") {
		t.Fatalf("expected missing-client error, got %v", err)
	}
}

func TestRequestCancelRequiresNamespaceAndName(t *testing.T) {
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
		err := (JobRunPublisher{Client: client}).RequestCancel(
			context.Background(), tc.namespace, tc.object)
		if err == nil || !strings.Contains(err.Error(), "job run namespace and name are required") {
			t.Fatalf("%s: expected identity error, got %v", tc.name, err)
		}
	}
}

func TestRequestCancelPatchesOnlyCancelRequested(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), seededJobRun("finance", "nightly-abc12345"))
	var patchBytes []byte
	client.PrependReactor("patch", "jobruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patchAction := action.(k8stesting.PatchAction)
		patchBytes = patchAction.GetPatch()
		return false, nil, nil
	})

	publisher := JobRunPublisher{Client: client}
	if err := publisher.RequestCancel(context.Background(), "finance", "nightly-abc12345"); err != nil {
		t.Fatalf("RequestCancel() error = %v", err)
	}

	// The patch must touch only spec.cancelRequested so it can never clobber
	// the rest of the spec the scheduler or the operator last wrote.
	if string(patchBytes) != cancelPatch {
		t.Fatalf("patch body = %s, want %s", patchBytes, cancelPatch)
	}
	if patchActionType(t, client) != types.MergePatchType {
		t.Fatal("cancel must be sent as a merge patch")
	}

	stored, err := client.Resource(jobRunGVR).Namespace("finance").
		Get(context.Background(), "nightly-abc12345", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("stored job run not found: %v", err)
	}
	cancelRequested, _, _ := unstructured.NestedBool(stored.Object, "spec", "cancelRequested")
	actor, _, _ := unstructured.NestedString(stored.Object, "spec", "actor")
	if !cancelRequested {
		t.Fatal("stored spec.cancelRequested = false, want true")
	}
	if actor != "original" {
		t.Fatalf("stored spec actor = %q, the patch must not change it", actor)
	}
}

// patchActionType returns the patch type of the recorded patch action, or
// fails the test when no patch reached the client.
func patchActionType(t *testing.T, client *dynamicfake.FakeDynamicClient) types.PatchType {
	t.Helper()
	for _, action := range client.Actions() {
		if patchAction, ok := action.(k8stesting.PatchAction); ok {
			return patchAction.GetPatchType()
		}
	}
	t.Fatal("no patch action reached the client")
	return ""
}

func TestRequestCancelMapsMissingResourceToConflict(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("patch", "jobruns", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(
			schema.GroupResource{Group: "workloads.orbitjob.io", Resource: "jobruns"},
			"nightly-abc12345")
	})

	err := (JobRunPublisher{Client: client}).RequestCancel(
		context.Background(), "finance", "nightly-abc12345")

	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected *resource.ConflictError, got %v", err)
	}
	if conflict.ID != "finance/nightly-abc12345" {
		t.Fatalf("conflict id = %v, want finance/nightly-abc12345", conflict.ID)
	}
}

func TestRequestCancelWrapsOtherErrors(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("patch", "jobruns", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("api server unavailable")
	})

	err := (JobRunPublisher{Client: client}).RequestCancel(
		context.Background(), "finance", "nightly-abc12345")

	var conflict *resource.ConflictError
	if errors.As(err, &conflict) {
		t.Fatalf("a transport failure is not a conflict: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "patch job run finance/nightly-abc12345") {
		t.Fatalf("expected wrapped patch error, got %v", err)
	}
}

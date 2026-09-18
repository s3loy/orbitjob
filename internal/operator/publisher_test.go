package operator

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
			Trigger:            v1alpha1.Schedule,
			OccurrenceKey:      "occ-1",
		},
	}
}

func TestPublishCreatesJobRun(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	if err := (RunPublisher{Client: client}).Publish(context.Background(), jobRunFor("finance", "nightly-abc12345")); err != nil {
		t.Fatal(err)
	}
	created, err := client.Resource(jobRunGVR).Namespace("finance").Get(context.Background(), "nightly-abc12345", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spec, found, _ := unstructured.NestedMap(created.Object, "spec")
	if !found || spec["occurrenceKey"] != "occ-1" {
		t.Fatalf("published spec = %v", spec)
	}
}

func TestPublishTreatsExistingRunAsSuccess(t *testing.T) {
	existing := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "workloads.orbitjob.io/v1alpha1",
		"kind":       "JobRun",
		"metadata":   map[string]any{"name": "nightly-abc12345", "namespace": "finance"},
		"spec":       map[string]any{"occurrenceKey": "occ-1"},
	}}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), existing)
	// The run row already exists, so the CR is a projection: re-publishing is
	// repair after a crash, not a conflict.
	if err := (RunPublisher{Client: client}).Publish(context.Background(), jobRunFor("finance", "nightly-abc12345")); err != nil {
		t.Fatalf("re-publish must converge: %v", err)
	}
}

func TestPublishRejectsIncompleteRun(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	if err := (RunPublisher{Client: client}).Publish(context.Background(), jobRunFor("", "name")); err == nil {
		t.Fatal("expected namespace validation")
	}
	if err := (RunPublisher{Client: client}).Publish(context.Background(), jobRunFor("finance", "")); err == nil {
		t.Fatal("expected name validation")
	}
	if err := (RunPublisher{}).Publish(context.Background(), jobRunFor("finance", "name")); err == nil {
		t.Fatal("expected client validation")
	}
}

func TestPublishPropagatesApiErrors(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("create", "jobruns", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "jobruns"}, "x", errors.New("denied"))
	})
	if err := (RunPublisher{Client: client}).Publish(context.Background(), jobRunFor("finance", "nightly-abc12345")); err == nil {
		t.Fatal("expected the API error to surface")
	}
}

func TestRemoveDerivesNameFromOccurrenceKey(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	if err := (RunRemover{Client: client}).Remove(context.Background(), "finance", "nightly", "abcdef1234567890"); err != nil {
		t.Fatal(err)
	}
	// The remover must compute the same name the publisher wrote, or retention
	// would delete nothing while claiming success.
	if err := (RunPublisher{Client: client}).Publish(context.Background(), jobRunFor("finance", "nightly-abcdef12")); err != nil {
		t.Fatal(err)
	}
	actions := client.Actions()
	if len(actions) != 2 || actions[0].GetVerb() != "delete" {
		t.Fatalf("actions = %v", actions)
	}
}

func TestRemoveTreatsAbsenceAsSuccess(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	if err := (RunRemover{Client: client}).Remove(context.Background(), "finance", "nightly", "abcdef1234567890"); err != nil {
		t.Fatalf("removing an absent run must converge: %v", err)
	}
}

func TestRemoveRejectsShortOccurrenceKey(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	// A short key cannot be truncated safely and would name the wrong object.
	if err := (RunRemover{Client: client}).Remove(context.Background(), "finance", "nightly", "abc"); err == nil {
		t.Fatal("expected a key length error")
	}
	if err := (RunRemover{}).Remove(context.Background(), "finance", "nightly", "abcdef1234567890"); err == nil {
		t.Fatal("expected a client error")
	}
}

func TestRemovePropagatesApiErrors(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("delete", "jobruns", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "jobruns"}, "x", errors.New("denied"))
	})
	if err := (RunRemover{Client: client}).Remove(context.Background(), "finance", "nightly", "abcdef1234567890"); err == nil {
		t.Fatal("expected the API error to surface so retention retries")
	}
}

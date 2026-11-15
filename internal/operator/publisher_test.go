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
		TypeMeta:   metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "JobRun"},
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
	typed := jobRunFor("finance", "nightly-abc12345")
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&typed)
	if err != nil {
		t.Fatal(err)
	}
	existing := &unstructured.Unstructured{Object: raw}
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
	key := "abcdef1234567890"
	run := jobRunFor("finance", v1alpha1.RunObjectName("nightly", key))
	run.Spec.OccurrenceKey = key
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	if err := (RunPublisher{Client: client}).Publish(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if err := (RunRemover{Client: client}).Remove(context.Background(), "finance", "nightly", key); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resource(jobRunGVR).Namespace("finance").Get(context.Background(), run.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("published occurrence survived retention: %v", err)
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
	key := "abcdef1234567890"
	run := jobRunFor("finance", v1alpha1.RunObjectName("nightly", key))
	run.Spec.OccurrenceKey = key
	if err := (RunPublisher{Client: client}).Publish(context.Background(), run); err != nil {
		t.Fatal(err)
	}

	client.PrependReactor("delete", "jobruns", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "jobruns"}, "x", errors.New("denied"))
	})
	if err := (RunRemover{Client: client}).Remove(context.Background(), "finance", "nightly", "abcdef1234567890"); err == nil {
		t.Fatal("expected the API error to surface so retention retries")
	}
}

func TestPublishRefusesForeignExecutionAndPreservesCancel(t *testing.T) {
	wanted := jobRunFor("finance", "nightly-12345678")
	for _, field := range []string{"occurrence", "revision", "source", "trigger", "actor", "timeout", "deleting", "cancel"} {
		t.Run(field, func(t *testing.T) {
			existing := wanted
			existing.TypeMeta = metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "JobRun"}
			switch field {
			case "occurrence":
				existing.Spec.OccurrenceKey = "different-full-key"
			case "revision":
				existing.Spec.DefinitionRevision = 99
			case "source":
				existing.Spec.ScheduledJobRef.UID = "foreign"
			case "trigger":
				existing.Spec.Trigger = v1alpha1.Manual
			case "actor":
				existing.Spec.Actor = "another-key"
			case "timeout":
				existing.Spec.TimeoutSeconds = 99
			case "deleting":
				at := metav1.Now()
				existing.DeletionTimestamp = &at
			case "cancel":
				existing.Spec.CancelRequested = true
			}
			raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&existing)
			if err != nil {
				t.Fatal(err)
			}
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), &unstructured.Unstructured{Object: raw})
			err = (RunPublisher{Client: client}).Publish(context.Background(), wanted)
			if field == "cancel" {
				if err != nil {
					t.Fatalf("identical canceled execution conflicted: %v", err)
				}
			} else if err == nil {
				t.Fatal("foreign execution was adopted")
			}
		})
	}
}

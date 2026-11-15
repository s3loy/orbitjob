package kube

import (
	"context"
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/domain/resource"
)

func TestPublishRejectsEveryForeignOccurrenceIdentity(t *testing.T) {
	base := jobRunFor("finance", "function-9-12345678")
	base.TypeMeta = metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "JobRun"}
	base.Spec.OccurrenceKey = "12345678" + strings.Repeat("a", 56)
	for _, tc := range []struct {
		name   string
		change func(*v1alpha1.JobRun)
	}{
		{"digest prefix collision", func(run *v1alpha1.JobRun) { run.Spec.OccurrenceKey = "12345678" + strings.Repeat("b", 56) }},
		{"foreign revision", func(run *v1alpha1.JobRun) { run.Spec.DefinitionRevision++ }},
		{"foreign source", func(run *v1alpha1.JobRun) { run.Spec.ScheduledJobRef.UID = "foreign" }},
		{"foreign trigger", func(run *v1alpha1.JobRun) { run.Spec.Trigger = v1alpha1.Function }},
		{"foreign actor", func(run *v1alpha1.JobRun) { run.Spec.Actor = "another-key" }},
		{"deleting object", func(run *v1alpha1.JobRun) { now := metav1.Now(); run.DeletionTimestamp = &now }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := base
			tc.change(&stored)
			raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&stored)
			if err != nil {
				t.Fatal(err)
			}
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), &unstructured.Unstructured{Object: raw})
			_, _, err = (JobRunPublisher{Client: client}).Publish(context.Background(), base)
			var conflict *resource.ConflictError
			if !errors.As(err, &conflict) {
				t.Fatalf("foreign occurrence adopted: %v", err)
			}
		})
	}
}

func TestPublishMatchingReplayPreservesCancelIntent(t *testing.T) {
	wanted := jobRunFor("finance", "nightly-12345678")
	wanted.TypeMeta = metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "JobRun"}
	stored := wanted
	stored.Spec.CancelRequested = true
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&stored)
	if err != nil {
		t.Fatal(err)
	}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), &unstructured.Unstructured{Object: raw})
	got, created, err := (JobRunPublisher{Client: client}).Publish(context.Background(), wanted)
	if err != nil || created || !got.Spec.CancelRequested {
		t.Fatalf("got=%+v created=%t error=%v", got, created, err)
	}
}

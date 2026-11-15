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

func TestWorkflowRunPublishRefusesSameNameForDifferentOccurrence(t *testing.T) {
	original := v1alpha1.WorkflowRun{TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "WorkflowRun"}, ObjectMeta: metav1.ObjectMeta{Name: "pipeline-12345678", Namespace: "tasks"}, Spec: v1alpha1.WorkflowRunSpec{WorkflowRef: v1alpha1.ObjectReference{Name: "pipeline", UID: "workflow-uid"}, DefinitionRevision: 5, OccurrenceKey: "12345678" + strings.Repeat("a", 56), Trigger: v1alpha1.Manual, Actor: "key-1"}}
	for _, field := range []string{"occurrence", "revision", "actor", "trigger", "source", "deleting"} {
		t.Run(field, func(t *testing.T) {
			raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&original)
			if err != nil {
				t.Fatal(err)
			}
			publisher := WorkflowRunPublisher{Client: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), &unstructured.Unstructured{Object: raw})}
			wanted := original
			switch field {
			case "occurrence":
				wanted.Spec.OccurrenceKey = "12345678" + strings.Repeat("b", 56)
			case "revision":
				wanted.Spec.DefinitionRevision = 6
			case "actor":
				wanted.Spec.Actor = "key-other"
			case "trigger":
				wanted.Spec.Trigger = v1alpha1.Schedule
			case "source":
				wanted.Spec.WorkflowRef.UID = "foreign"
			case "deleting":
				at := metav1.Now()
				existingRaw := original
				existingRaw.DeletionTimestamp = &at
				raw, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(&existingRaw)
				publisher.Client = dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), &unstructured.Unstructured{Object: raw})
			}
			_, _, err = publisher.Create(context.Background(), wanted)
			var conflict *resource.ConflictError
			if !errors.As(err, &conflict) {
				t.Fatalf("foreign execution was adopted: %v", err)
			}
		})
	}
	// Retrying an already-canceled identical execution keeps its original receipt.
	original.Spec.CancelRequested = true
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&original)
	if err != nil {
		t.Fatal(err)
	}
	publisher := WorkflowRunPublisher{Client: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), &unstructured.Unstructured{Object: raw})}
	wanted := original
	wanted.Spec.CancelRequested = false
	if got, created, err := publisher.Create(context.Background(), wanted); err != nil || created || !got.Spec.CancelRequested {
		t.Fatalf("identical canceled replay=%+v created=%v err=%v", got, created, err)
	}
}

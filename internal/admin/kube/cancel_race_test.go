package kube

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
	v1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

func TestCancellationCannotPatchARecreatedOrChangedRun(t *testing.T) {
	for _, workflow := range []bool{false, true} {
		for _, changed := range []string{"uid", "resourceVersion"} {
			t.Run(map[bool]string{false: "jobrun", true: "workflowrun"}[workflow]+"-"+changed, func(t *testing.T) {
				id := v1.RunIdentity{SourceUID: "source-a", RevisionID: 1, OccurrenceKey: strings.Repeat("a", 64), Trigger: v1.Manual, Workflow: workflow}
				resource, kind, ref := "jobruns", "JobRun", "scheduledJobRef"
				if workflow {
					resource, kind, ref = "workflowruns", "WorkflowRun", "workflowRef"
				}
				gvr := schema.GroupVersionResource{Group: v1.GroupVersion.Group, Version: v1.GroupVersion.Version, Resource: resource}
				name := v1.RunObjectName("definition", id.OccurrenceKey)
				object := &unstructured.Unstructured{Object: map[string]any{"apiVersion": v1.GroupVersion.String(), "kind": kind, "metadata": map[string]any{"name": name, "namespace": "ns", "uid": "original-uid", "resourceVersion": "1"}, "spec": map[string]any{ref: map[string]any{"uid": id.SourceUID}, "occurrenceKey": id.OccurrenceKey, "definitionRevision": int64(1), "trigger": "Manual", "actor": "original"}}}
				replacement := object.DeepCopy()
				if changed == "uid" {
					replacement.SetUID("replacement-uid")
				} else {
					replacement.SetResourceVersion("2")
				}
				_ = unstructured.SetNestedField(replacement.Object, "foreign-source", "spec", ref, "uid")
				client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), object)
				patches := 0
				client.PrependReactor("patch", resource, func(k8stesting.Action) (bool, runtime.Object, error) {
					patches++
					if err := client.Tracker().Update(gvr, replacement, "ns"); err != nil {
						t.Fatal(err)
					}
					return false, nil, nil
				})
				var err error
				if workflow {
					_, err = (WorkflowRunPublisher{Client: client}).RequestCancel(t.Context(), "ns", "definition", id)
				} else {
					_, err = (JobRunPublisher{Client: client}).RequestCancel(t.Context(), "ns", "definition", id)
				}
				if err == nil {
					t.Error("cancellation succeeded after the validated object changed")
				}
				stored, getErr := client.Resource(gvr).Namespace("ns").Get(t.Context(), name, metav1.GetOptions{})
				if getErr != nil {
					t.Fatal(getErr)
				}
				cancelled, _, _ := unstructured.NestedBool(stored.Object, "spec", "cancelRequested")
				if cancelled {
					t.Error("cancellation reached the replacement object")
				}
				if patches != 1 {
					t.Fatalf("unsafe automatic retry count=%d", patches)
				}
			})
		}
	}
}

func TestMatchingCancellationRemainsIdempotentForBothRunKinds(t *testing.T) {
	for _, workflow := range []bool{false, true} {
		t.Run(map[bool]string{false: "jobrun", true: "workflowrun"}[workflow], func(t *testing.T) {
			id := v1.RunIdentity{SourceUID: "source", RevisionID: 1, OccurrenceKey: "idempotent", Trigger: v1.Manual, Workflow: workflow}
			resource, kind, ref := "jobruns", "JobRun", "scheduledJobRef"
			if workflow {
				resource, kind, ref = "workflowruns", "WorkflowRun", "workflowRef"
			}
			name := v1.RunObjectName("definition", id.OccurrenceKey)
			object := &unstructured.Unstructured{Object: map[string]any{"apiVersion": v1.GroupVersion.String(), "kind": kind, "metadata": map[string]any{"name": name, "namespace": "ns", "uid": "uid", "resourceVersion": "1"}, "spec": map[string]any{ref: map[string]any{"uid": id.SourceUID}, "occurrenceKey": id.OccurrenceKey, "definitionRevision": int64(1), "trigger": "Manual", "actor": "original"}}}
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), object)
			for range 2 {
				var actual string
				var err error
				if workflow {
					actual, err = (WorkflowRunPublisher{Client: client}).RequestCancel(t.Context(), "ns", "definition", id)
				} else {
					actual, err = (JobRunPublisher{Client: client}).RequestCancel(t.Context(), "ns", "definition", id)
				}
				if err != nil || actual != name {
					t.Fatalf("cancel name=%s err=%v", actual, err)
				}
			}
			stored, err := client.Resource(schema.GroupVersionResource{Group: v1.GroupVersion.Group, Version: v1.GroupVersion.Version, Resource: resource}).Namespace("ns").Get(t.Context(), name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			cancelled, _, _ := unstructured.NestedBool(stored.Object, "spec", "cancelRequested")
			actor, _, _ := unstructured.NestedString(stored.Object, "spec", "actor")
			if !cancelled || actor != "original" {
				t.Fatalf("cancelled=%v actor=%s", cancelled, actor)
			}
		})
	}
}

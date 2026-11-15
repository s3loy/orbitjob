package operator

import (
	"errors"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
	v1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

func TestIdentityRemovalUsesUIDAndResourceVersionAtomically(t *testing.T) {
	for _, workflow := range []bool{false, true} {
		for _, scenario := range []string{"matching", "same-uid-spec-change", "replacement-uid", "status-update"} {
			t.Run(fmt.Sprintf("workflow=%v/%s", workflow, scenario), func(t *testing.T) {
				id := v1.RunIdentity{SourceUID: "source", RevisionID: 1, OccurrenceKey: "occurrence", Trigger: v1.Manual, Workflow: workflow}
				resource, kind, ref := "jobruns", "JobRun", "scheduledJobRef"
				if workflow {
					resource, kind, ref = "workflowruns", "WorkflowRun", "workflowRef"
				}
				gvr := schema.GroupVersionResource{Group: v1.GroupVersion.Group, Version: v1.GroupVersion.Version, Resource: resource}
				name := v1.RunObjectName("definition", id.OccurrenceKey)
				original := &unstructured.Unstructured{Object: map[string]any{"apiVersion": v1.GroupVersion.String(), "kind": kind, "metadata": map[string]any{"name": name, "namespace": "ns", "uid": "original", "resourceVersion": "1"}, "spec": map[string]any{ref: map[string]any{"uid": "source"}, "occurrenceKey": id.OccurrenceKey, "definitionRevision": int64(1), "trigger": "Manual"}}}
				client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), original)
				deletes := 0
				client.PrependReactor("delete", resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
					deletes++
					live := original.DeepCopy()
					if scenario != "matching" {
						live.SetResourceVersion("2")
					}
					if scenario == "same-uid-spec-change" {
						_ = unstructured.SetNestedField(live.Object, "other-source", "spec", ref, "uid")
					}
					if scenario == "replacement-uid" {
						live.SetUID("replacement")
					}
					if err := client.Tracker().Update(gvr, live, "ns"); err != nil {
						t.Fatal(err)
					}
					conditions := action.(k8stesting.DeleteAction).GetDeleteOptions().Preconditions
					if conditions == nil || conditions.UID == nil || conditions.ResourceVersion == nil {
						t.Error("delete did not carry both UID and RV preconditions")
					}
					if conditions != nil && ((conditions.UID != nil && *conditions.UID != live.GetUID()) || (conditions.ResourceVersion != nil && *conditions.ResourceVersion != live.GetResourceVersion())) {
						return true, nil, apierrors.NewConflict(gvr.GroupResource(), name, errors.New("server precondition failed"))
					}
					return false, nil, nil
				})
				remover := RunRemover{Client: client}
				err := remover.RemoveIdentity(t.Context(), "ns", "definition", id)
				_, getErr := client.Resource(gvr).Namespace("ns").Get(t.Context(), name, metav1.GetOptions{})
				if scenario == "matching" {
					if err != nil || !apierrors.IsNotFound(getErr) {
						t.Fatalf("matching removal err=%v get=%v", err, getErr)
					}
				} else {
					if !apierrors.IsConflict(err) || getErr != nil {
						t.Fatalf("changed object was not preserved: err=%v get=%v", err, getErr)
					}
				}
				if deletes != 1 {
					t.Fatalf("delete retried without next retention identity check: %d", deletes)
				}
				if scenario == "status-update" {
					if err := remover.RemoveIdentity(t.Context(), "ns", "definition", id); err != nil {
						t.Fatalf("next sweep cannot remove matching updated object: %v", err)
					}
				}
			})
		}
	}
}

package v1alpha1

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
)

func TestCancelRunRetriesOnlyTheSameIdentityAndUID(t *testing.T) {
	for _, scenario := range []string{"same-uid-status-update", "replacement-uid", "changed-spec", "continuously-changing"} {
		t.Run(scenario, func(t *testing.T) {
			expected := RunIdentity{SourceUID: "source", OccurrenceKey: "occurrence", RevisionID: 1, Trigger: Manual}
			name := RunObjectName("definition", expected.OccurrenceKey)
			gvr := schema.GroupVersionResource{Group: GroupVersion.Group, Version: GroupVersion.Version, Resource: "jobruns"}
			object := &unstructured.Unstructured{Object: map[string]any{"apiVersion": GroupVersion.String(), "kind": "JobRun", "metadata": map[string]any{"name": name, "namespace": "ns", "uid": "uid", "resourceVersion": "1"}, "spec": map[string]any{"scheduledJobRef": map[string]any{"uid": "source"}, "occurrenceKey": "occurrence", "definitionRevision": int64(1), "trigger": "Manual"}}}
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), object)
			patches := 0
			client.PrependReactor("patch", "jobruns", func(k8stesting.Action) (bool, runtime.Object, error) {
				patches++
				if patches > 1 && scenario != "continuously-changing" {
					return false, nil, nil
				}
				live := object.DeepCopy()
				live.SetResourceVersion(fmt.Sprint(patches + 1))
				if scenario == "replacement-uid" {
					live.SetUID("replacement")
				}
				if scenario == "changed-spec" {
					_ = unstructured.SetNestedField(live.Object, "Retry", "spec", "trigger")
				}
				if err := client.Tracker().Update(gvr, live, "ns"); err != nil {
					t.Fatal(err)
				}
				return true, nil, apierrors.NewConflict(gvr.GroupResource(), name, errors.New("concurrent resource version update"))
			})
			actual, err := CancelRun(t.Context(), client.Resource(gvr).Namespace("ns"), RunNameCandidates("definition", expected.OccurrenceKey), expected)
			stored, getErr := client.Resource(gvr).Namespace("ns").Get(t.Context(), name, metav1.GetOptions{})
			if getErr != nil {
				t.Fatal(getErr)
			}
			cancelled, _, _ := unstructured.NestedBool(stored.Object, "spec", "cancelRequested")
			if scenario == "same-uid-status-update" {
				if err != nil || actual != name || !cancelled || patches != 2 {
					t.Fatalf("retry name=%s patches=%d cancelled=%v err=%v", actual, patches, cancelled, err)
				}
			} else {
				if !errors.Is(err, ErrRunIdentityConflict) || cancelled {
					t.Fatalf("unsafe retry cancelled=%v err=%v", cancelled, err)
				}
				max := 1
				if scenario == "continuously-changing" {
					max = 3
				}
				if patches != max {
					t.Fatalf("patch attempts=%d want=%d", patches, max)
				}
			}
		})
	}
}

package operator

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
	v1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

func TestWorkflowFanoutCannotCancelAReplacementStep(t *testing.T) {
	id := v1.RunIdentity{SourceUID: "def-1", OccurrenceKey: "occ-1", RevisionID: 7, Trigger: v1.Schedule}
	name := v1.RunObjectName("nightly", id.OccurrenceKey)
	run := jobRunFor("ns", name)
	run.UID = "original"
	run.ResourceVersion = "1"
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&run)
	if err != nil {
		t.Fatal(err)
	}
	replacement := &unstructured.Unstructured{Object: raw}
	replacement.SetUID("replacement")
	replacement.SetResourceVersion("2")
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), &run)
	client.PrependReactor("patch", "jobruns", func(k8stesting.Action) (bool, runtime.Object, error) {
		if err := client.Tracker().Update(jobRunGVR, replacement, "ns"); err != nil {
			t.Fatal(err)
		}
		return false, nil, nil
	})
	if err := (RunPublisher{Client: client}).RequestCancel(t.Context(), "ns", "nightly", id); err == nil {
		t.Error("fanout accepted replacement step")
	}
	stored, err := client.Resource(jobRunGVR).Namespace("ns").Get(t.Context(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, _, _ := unstructured.NestedBool(stored.Object, "spec", "cancelRequested")
	if cancelled {
		t.Fatal("fanout cancelled replacement step")
	}
}

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
	ktesting "k8s.io/client-go/testing"
)

func TestDynamicHandlerRoutesResources(t *testing.T) {
	for _, tc := range []struct {
		key, apiVersion, kind string
		gvr                   schema.GroupVersionResource
	}{
		{"scheduledjobs:finance/report", "workloads.orbitjob.io/v1alpha1", "ScheduledJob", scheduledJobGVR},
		{"jobruns:finance/report", "workloads.orbitjob.io/v1alpha1", "JobRun", jobRunGVR},
		{"jobs:finance/report", "batch/v1", "Job", jobGVR},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": tc.apiVersion, "kind": tc.kind, "metadata": map[string]interface{}{"name": "report", "namespace": "finance"}}}
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), obj)
			calls := 0
			callback := func(_ context.Context, got unstructured.Unstructured) error {
				calls++
				if got.GetKind() != tc.kind {
					t.Fatalf("wrong kind %s", got.GetKind())
				}
				return nil
			}
			h := DynamicHandler{Client: client, ReconcileScheduledJob: callback, ReconcileJobRun: callback, ReconcileJob: callback}
			if err := h.Handle(context.Background(), tc.key); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("calls=%d", calls)
			}
			actions := client.Actions()
			if len(actions) != 1 || actions[0].GetResource() != tc.gvr {
				t.Fatalf("actions=%v", actions)
			}
		})
	}
}

func TestDynamicHandlerPropagatesErrors(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	forbidden := apierrors.NewForbidden(scheduledJobGVR.GroupResource(), "report", errors.New("denied"))
	client.PrependReactor("get", "scheduledjobs", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, forbidden })
	h := DynamicHandler{Client: client, ReconcileScheduledJob: func(context.Context, unstructured.Unstructured) error { t.Fatal("unexpected callback"); return nil }}
	if err := h.Handle(context.Background(), "scheduledjobs:finance/report"); !apierrors.IsForbidden(err) {
		t.Fatalf("error=%v", err)
	}
}

func TestDynamicHandlerDeletedResource(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	called := false
	h := DynamicHandler{Client: client, ReconcileScheduledJob: func(context.Context, unstructured.Unstructured) error { called = true; return nil }}
	if err := h.Handle(context.Background(), "scheduledjobs:finance/missing"); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("callback called for deleted object")
	}
}

func TestDynamicHandlerRejectsInvalidKeys(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	for _, key := range []string{"", "finance/report", "jobs:/report", "jobs:finance/", "jobs:finance/report/extra", "secrets:finance/report"} {
		if err := (DynamicHandler{Client: client}).Handle(context.Background(), key); err == nil {
			t.Errorf("accepted %q", key)
		}
	}
	if len(client.Actions()) != 0 {
		t.Fatal("invalid keys accessed API")
	}
}

var _ = metav1.GetOptions{}

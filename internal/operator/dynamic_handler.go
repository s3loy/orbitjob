package operator

import (
	"context"
	"fmt"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"strings"
)

type DynamicHandler struct {
	Client                dynamic.Interface
	ReconcileScheduledJob func(context.Context, unstructured.Unstructured) error
	ReconcileJobRun       func(context.Context, unstructured.Unstructured) error
	ReconcileJob          func(context.Context, unstructured.Unstructured) error
	ReconcileWorkflowRun  func(context.Context, unstructured.Unstructured) error
	ReconcileWorkflowJob  func(context.Context, unstructured.Unstructured) error
}

func (h DynamicHandler) Handle(ctx context.Context, key string) error {
	if h.Client == nil {
		return fmt.Errorf("dynamic client is required")
	}
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid resource key")
	}
	resource := parts[0]
	p := strings.SplitN(parts[1], "/", 2)
	if len(p) != 2 {
		return fmt.Errorf("invalid resource key")
	}
	var fn func(context.Context, unstructured.Unstructured) error
	switch resource {
	case "scheduledjobs":
		fn = h.ReconcileScheduledJob
	case "jobruns":
		fn = h.ReconcileJobRun
	case "jobs":
		fn = h.ReconcileJob
	case "workflowruns":
		fn = h.ReconcileWorkflowRun
	case "workflowjobs":
		fn = h.ReconcileWorkflowJob
	}
	if fn == nil {
		return fmt.Errorf("reconcile handler is required")
	}
	gvr := schema.GroupVersionResource{Group: "workloads.orbitjob.io", Version: "v1alpha1", Resource: resource}
	if resource == "jobs" {
		gvr = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
	}
	obj, err := h.Client.Resource(gvr).Namespace(p[0]).Get(ctx, p[1], metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return fn(ctx, *obj)
}

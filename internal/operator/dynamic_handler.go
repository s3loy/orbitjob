package operator

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type DynamicHandler struct {
	Client                dynamic.Interface
	ReconcileScheduledJob func(context.Context, unstructured.Unstructured) error
	ReconcileJobRun       func(context.Context, unstructured.Unstructured) error
	ReconcileJob          func(context.Context, unstructured.Unstructured) error
	ReconcileWorkflowRun  func(context.Context, unstructured.Unstructured) error
	ReconcileWorkflowJob  func(context.Context, unstructured.Unstructured) error
}

// Handle routes one reconcile key to its handler. The object arrives from the
// informer's store when cached is set; otherwise (deleted while queued, or a
// call from outside the watch path) one read of the API server decides, and
// NotFound ends the reconcile as complete rather than failed.
func (h DynamicHandler) Handle(ctx context.Context, key string, obj unstructured.Unstructured, cached bool) error {
	if h.Client == nil {
		return fmt.Errorf("dynamic client is required")
	}
	resource, nsname, ok := strings.Cut(key, ":")
	if !ok {
		return fmt.Errorf("invalid resource key")
	}
	namespace, name, ok := strings.Cut(nsname, "/")
	if !ok || namespace == "" || name == "" {
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
	if cached {
		return fn(ctx, obj)
	}
	current, err := h.Client.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return fn(ctx, *current)
}

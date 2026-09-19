package operator

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

// RunPublisher exposes a stored run as a JobRun custom resource.
type RunPublisher struct {
	Client dynamic.Interface
}

// Publish creates the JobRun CR if it is missing. An AlreadyExists result is
// success: the run row is the source of truth, and the CR is a projection of it,
// so a repeated publish is repair rather than a conflict.
func (p RunPublisher) Publish(ctx context.Context, run v1alpha1.JobRun) error {
	if p.Client == nil {
		return fmt.Errorf("dynamic client is required")
	}
	if run.Namespace == "" || run.Name == "" {
		return fmt.Errorf("job run namespace and name are required")
	}
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&run)
	if err != nil {
		return fmt.Errorf("encode job run %s: %w", run.Name, err)
	}
	_, err = p.Client.Resource(jobRunGVR).Namespace(run.Namespace).
		Create(ctx, &unstructured.Unstructured{Object: object}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create job run %s/%s: %w", run.Namespace, run.Name, err)
	}
	return nil
}

// cancelRequestPatch is the merge patch a stop request sends. It touches only
// spec.cancelRequested — a request, not a fact: the run reaches Canceled only
// after its Kubernetes Job is observed gone.
const cancelRequestPatch = `{"spec":{"cancelRequested":true}}`

// RequestCancel patches a run CR's spec.cancelRequested. It is the walker's
// cancel fan-out primitive: each in-flight step of a stopping workflow run is
// asked to stop here, and the ordinary run reconciliation turns the request
// into an observed Canceled. NotFound is success — a CR that is gone cannot
// start more work.
func (p RunPublisher) RequestCancel(ctx context.Context, namespace, name string) error {
	if p.Client == nil {
		return fmt.Errorf("dynamic client is required")
	}
	if namespace == "" || name == "" {
		return fmt.Errorf("job run namespace and name are required")
	}
	_, err := p.Client.Resource(jobRunGVR).Namespace(namespace).
		Patch(ctx, name, types.MergePatchType, []byte(cancelRequestPatch), metav1.PatchOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("patch cancel request on job run %s/%s: %w", namespace, name, err)
	}
	return nil
}

// RunRemover deletes JobRun custom resources that retention has pruned.
type RunRemover struct {
	Client dynamic.Interface
}

// Remove deletes the JobRun CR named after the occurrence key. The name is
// derived the same way the publisher derived it, so retention does not need an
// index of published objects.
func (r RunRemover) Remove(ctx context.Context, namespace, scheduledJobName, occurrenceKey string) error {
	if r.Client == nil {
		return fmt.Errorf("dynamic client is required")
	}
	if len(occurrenceKey) < 8 {
		return fmt.Errorf("occurrence key %q is too short to derive a run name", occurrenceKey)
	}
	name := scheduledJobName + "-" + occurrenceKey[:8]
	err := r.Client.Resource(jobRunGVR).Namespace(namespace).
		Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete job run %s/%s: %w", namespace, name, err)
	}
	return nil
}

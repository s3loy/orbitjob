package execution

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// JobClient is the subset of the Kubernetes Job API the control plane uses.
type JobClient interface {
	Get(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*batchv1.Job, error)
	Create(ctx context.Context, job *batchv1.Job, opts metav1.CreateOptions) (*batchv1.Job, error)
	Delete(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error
}

// Adapter drives Kubernetes Jobs for one attempt at a time.
type Adapter struct {
	Client JobClient
}

// Ensure returns the Job for a desired spec, creating it if absent. It is
// idempotent by namespace and name, which is what lets a controller restart
// adopt the Job it created before crashing rather than starting a second one.
func (a Adapter) Ensure(ctx context.Context, desired *batchv1.Job) (*batchv1.Job, error) {
	if a.Client == nil {
		return nil, fmt.Errorf("job client is required")
	}
	if desired == nil || desired.Name == "" || desired.Namespace == "" {
		return nil, fmt.Errorf("desired job identity is required")
	}
	existing, err := a.Client.Get(ctx, desired.Namespace, desired.Name, metav1.GetOptions{})
	if err == nil {
		return existing, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}
	created, err := a.Client.Create(ctx, desired, metav1.CreateOptions{})
	if err == nil {
		return created, nil
	}
	// Lost a create race: adopt the winner rather than failing the reconcile.
	if !apierrors.IsAlreadyExists(err) {
		return nil, err
	}
	return a.Client.Get(ctx, desired.Namespace, desired.Name, metav1.GetOptions{})
}

// Get reports the live Job, or found=false when it is gone. Absence is a normal
// answer: it is how a cancelled or garbage-collected Job is observed.
func (a Adapter) Get(ctx context.Context, namespace, name string) (*batchv1.Job, bool, error) {
	if a.Client == nil {
		return nil, false, fmt.Errorf("job client is required")
	}
	job, err := a.Client.Get(ctx, namespace, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return job, true, nil
}

// Delete stops a Job and everything it owns. Absence is success so that a
// repeated cancel request converges instead of erroring.
func (a Adapter) Delete(ctx context.Context, namespace, name string) error {
	if a.Client == nil {
		return fmt.Errorf("job client is required")
	}
	policy := metav1.DeletePropagationBackground
	err := a.Client.Delete(ctx, namespace, name, metav1.DeleteOptions{PropagationPolicy: &policy})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

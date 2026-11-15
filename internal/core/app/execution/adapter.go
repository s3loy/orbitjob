package execution

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

// Ensure performs the one creation authorized by the durable dispatch marker.
// Existing resources converge by namespace/name. After an ambiguous call the
// caller must only Get-observe; another Ensure could recreate a removed Job.
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
func (a Adapter) Delete(ctx context.Context, namespace, name string, uid ...string) error {
	if a.Client == nil {
		return fmt.Errorf("job client is required")
	}
	policy := metav1.DeletePropagationForeground
	opts := metav1.DeleteOptions{PropagationPolicy: &policy}
	if len(uid) > 0 && uid[0] != "" {
		owned := types.UID(uid[0])
		opts.Preconditions = &metav1.Preconditions{UID: &owned}
	}
	err := a.Client.Delete(ctx, namespace, name, opts)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// Stopped confirms all matching workload Pods terminal after the Job is gone.
// A missing observer is an error, never evidence that the workload stopped.
func (a Adapter) Stopped(ctx context.Context, namespace, name string) (bool, error) {
	if _, found, err := a.Get(ctx, namespace, name); err != nil || found {
		return false, err
	}
	observer, ok := a.Client.(interface {
		PodsForJob(context.Context, string, string) ([]corev1.Pod, error)
	})
	if !ok {
		return false, fmt.Errorf("workload Pod observer is required")
	}
	pods, err := observer.PodsForJob(ctx, namespace, name)
	if err != nil {
		return false, err
	}
	for _, pod := range pods {
		if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
			return false, nil
		}
	}
	return true, nil
}

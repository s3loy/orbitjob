package execution

import (
	"context"
	"errors"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	typedbatch "k8s.io/client-go/kubernetes/typed/batch/v1"
)

var errNamespaceRequired = errors.New("kubernetes job namespace is required")

// KubernetesJobClient adapts the generated clientset to the JobClient interface.
// The adapter exists so tests can substitute a fake without dragging the whole
// clientset in, and so the domain path never depends on generated types.
type KubernetesJobClient struct {
	Client typedbatch.BatchV1Interface
}

func (c KubernetesJobClient) Get(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*batchv1.Job, error) {
	return c.Client.Jobs(namespace).Get(ctx, name, opts)
}

func (c KubernetesJobClient) Create(ctx context.Context, job *batchv1.Job, opts metav1.CreateOptions) (*batchv1.Job, error) {
	if job.Namespace == "" {
		return nil, errNamespaceRequired
	}
	return c.Client.Jobs(job.Namespace).Create(ctx, job, opts)
}

// Delete removes the Job and, with background propagation, the Pods it owns.
func (c KubernetesJobClient) Delete(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error {
	return c.Client.Jobs(namespace).Delete(ctx, name, opts)
}

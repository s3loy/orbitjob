package execution

import (
	"context"
	"errors"

	"fmt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	typedbatch "k8s.io/client-go/kubernetes/typed/batch/v1"
	typedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

var errNamespaceRequired = errors.New("kubernetes job namespace is required")

// KubernetesJobClient adapts the generated clientset to the JobClient interface.
// The adapter exists so tests can substitute a fake without dragging the whole
// clientset in, and so the domain path never depends on generated types.
type KubernetesJobClient struct {
	Client typedbatch.BatchV1Interface
	Core   typedcore.CoreV1Interface
}

func (c KubernetesJobClient) Get(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*batchv1.Job, error) {
	return c.Client.Jobs(namespace).Get(ctx, name, opts)
}

func (c KubernetesJobClient) Create(ctx context.Context, job *batchv1.Job, opts metav1.CreateOptions) (*batchv1.Job, error) {
	if job.Namespace == "" {
		return nil, errNamespaceRequired
	}
	client := c.Client.RESTClient()
	// Generated fake clientsets expose a typed nil REST client and perform no
	// transport retries. Keep their ordinary action tracking for unit callers.
	if concrete, ok := client.(*rest.RESTClient); ok && concrete == nil {
		client = nil
	}
	if client == nil {
		return c.Client.Jobs(job.Namespace).Create(ctx, job, opts)
	}
	// Even Retry-After must not replay a possibly accepted POST. The next
	// controller pass observes the original dispatch instead of resending it.
	result := &batchv1.Job{}
	err := client.Post().Namespace(job.Namespace).Resource("jobs").
		VersionedParams(&opts, scheme.ParameterCodec).Body(job).MaxRetries(0).
		Do(ctx).Into(result)
	return result, err
}

// Delete passes through the caller's propagation policy and UID preconditions.
func (c KubernetesJobClient) Delete(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error {
	return c.Client.Jobs(namespace).Delete(ctx, name, opts)
}

func (c KubernetesJobClient) PodsForJob(ctx context.Context, namespace, name string) ([]corev1.Pod, error) {
	if c.Core == nil {
		return nil, fmt.Errorf("core Kubernetes client is required for Pod observation")
	}
	pods, err := c.Core.Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: batchv1.JobNameLabel + "=" + name})
	if err != nil {
		return nil, err
	}
	return pods.Items, nil
}

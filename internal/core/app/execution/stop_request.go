package execution

import (
	"context"
	"encoding/json"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// RequestStop shortens the original Job's deadline. It requests controller
// acknowledgement; neither the patch response nor elapsed time proves a stop.
func (a Adapter) RequestStop(ctx context.Context, job *batchv1.Job) (bool, error) {
	if job == nil || job.Name == "" || job.Namespace == "" || job.UID == "" || job.ResourceVersion == "" {
		return false, fmt.Errorf("stop request requires Job namespace, name, UID and resourceVersion")
	}
	if job.DeletionTimestamp != nil {
		return false, fmt.Errorf("deleting Job cannot accept a new stop request")
	}
	if job.Spec.ActiveDeadlineSeconds != nil && *job.Spec.ActiveDeadlineSeconds <= 1 {
		return false, nil
	}
	client, ok := a.Client.(interface {
		Patch(context.Context, string, string, types.PatchType, []byte, metav1.PatchOptions) (*batchv1.Job, error)
	})
	if !ok {
		return false, fmt.Errorf("guarded Job patch client required")
	}
	patch, err := json.Marshal([]map[string]any{
		{"op": "test", "path": "/metadata/uid", "value": string(job.UID)},
		{"op": "test", "path": "/metadata/resourceVersion", "value": job.ResourceVersion},
		{"op": "add", "path": "/spec/activeDeadlineSeconds", "value": 1},
	})
	if err != nil {
		return false, err
	}
	_, err = client.Patch(ctx, job.Namespace, job.Name, types.JSONPatchType, patch, metav1.PatchOptions{})
	return err == nil, err
}

func (c KubernetesJobClient) Patch(ctx context.Context, namespace, name string, kind types.PatchType, data []byte, opts metav1.PatchOptions) (*batchv1.Job, error) {
	return c.Client.Jobs(namespace).Patch(ctx, name, kind, data, opts)
}

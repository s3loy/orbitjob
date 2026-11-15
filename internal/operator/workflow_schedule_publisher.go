package operator

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

// PublishWorkflow adopts only an identical occurrence and returns its immutable
// instance UID for ledger binding. Once bound, a missing or replacement object
// is an explicit conflict; it cannot silently become another execution object.
func (p RunPublisher) PublishWorkflow(ctx context.Context, run v1alpha1.WorkflowRun) (string, error) {
	if p.Client == nil {
		return "", fmt.Errorf("dynamic client is required")
	}
	if run.Namespace == "" || run.Name == "" {
		return "", fmt.Errorf("workflow run namespace and name are required")
	}
	if err := run.Spec.Validate(); err != nil {
		return "", err
	}
	client := p.Client.Resource(workflowRunGVR).Namespace(run.Namespace)
	var existing *unstructured.Unstructured
	var err error
	if run.UID != "" {
		existing, err = client.Get(ctx, run.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("read original workflow run instance: %w", err)
		}
	} else {
		raw, encodeErr := runtime.DefaultUnstructuredConverter.ToUnstructured(&run)
		if encodeErr != nil {
			return "", encodeErr
		}
		existing, err = client.Create(ctx, &unstructured.Unstructured{Object: raw}, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			existing, err = client.Get(ctx, run.Name, metav1.GetOptions{})
		}
		if err != nil {
			return "", err
		}
	}
	var stored v1alpha1.WorkflowRun
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(existing.Object, &stored); err != nil {
		return "", err
	}
	if stored.UID == "" || stored.DeletionTimestamp != nil || (run.UID != "" && run.UID != stored.UID) {
		return "", fmt.Errorf("workflow run %s/%s original UID is unavailable or replaced", run.Namespace, run.Name)
	}
	wantedSpec, existingSpec := run.Spec, stored.Spec
	wantedSpec.CancelRequested, existingSpec.CancelRequested = false, false
	want, _ := json.Marshal(wantedSpec)
	got, _ := json.Marshal(existingSpec)
	if string(want) != string(got) {
		return "", fmt.Errorf("workflow run %s/%s identity conflicts with recorded occurrence", run.Namespace, run.Name)
	}
	return string(stored.UID), nil
}

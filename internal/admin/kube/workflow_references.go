package kube

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/domain/validation"
)

// Check both dry-runs and writes against the same mapped namespace. A dry-run
// receipt cannot guarantee a referenced definition survives until execution;
// the walker records that later failure independently.
func (d DefinitionClient) validateWorkflowReferences(ctx context.Context, namespace, canonical string) error {
	var spec v1.WorkflowJobSpec
	if err := json.Unmarshal([]byte(canonical), &spec); err != nil {
		return err
	}
	gvr, _, _ := definitionResource("jobs")
	seen := make(map[string]bool)
	for i, task := range spec.Tasks {
		name := task.JobRef.Name
		if seen[name] {
			continue
		}
		obj, err := d.Client.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) || (err == nil && obj.GetDeletionTimestamp() != nil) {
			return validation.New(fmt.Sprintf("spec.tasks[%d].jobRef.name", i), fmt.Sprintf("ScheduledJob %q is unavailable in namespace %q; create it first or select an existing Job", name, namespace))
		}
		if err != nil {
			return definitionError(err)
		}
		seen[name] = true
	}
	return nil
}

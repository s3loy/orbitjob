package operator

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

// RemoveWorkflow removes only the terminal resource belonging to a retained
// ledger row. UID and resourceVersion preconditions protect against replacement
// or concurrent edits between the read and the delete. The row is pruned later.
func (r RunRemover) RemoveWorkflow(ctx context.Context, definition revision.Identity, row workflow.Run) error {
	if r.Client == nil {
		return fmt.Errorf("dynamic client is required")
	}
	if !workflow.Terminal(row.Phase) {
		return fmt.Errorf("refuse removal of nonterminal workflow run %d", row.ID)
	}
	client := r.Client.Resource(workflowRunGVR).Namespace(definition.Namespace)
	obj, found, err := v1alpha1.ResolveRun(ctx, client, v1alpha1.RunNameCandidates(definition.Name, row.OccurrenceKey),
		v1alpha1.RunIdentity{SourceUID: row.SourceUID, OccurrenceKey: row.OccurrenceKey, RevisionID: row.RevisionID, Trigger: v1alpha1.Trigger(row.Trigger), Workflow: true})
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	name := obj.GetName()
	var run v1alpha1.WorkflowRun
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &run); err != nil {
		return err
	}
	if row.ResourceUID == "" || string(run.UID) != row.ResourceUID || run.UID == "" || run.ResourceVersion == "" || run.DeletionTimestamp != nil {
		return fmt.Errorf("workflow run %s/%s has no stable deletion identity", definition.Namespace, name)
	}
	if definition.SourceMode != workflow.SourceModeWorkflow || definition.SourceUID != row.SourceUID ||
		run.Namespace != definition.Namespace || run.Name != name || run.Spec.WorkflowRef.Name != definition.Name ||
		run.Spec.WorkflowRef.UID != row.SourceUID || run.Spec.DefinitionRevision != row.RevisionID ||
		run.Spec.OccurrenceKey != row.OccurrenceKey || string(run.Spec.Trigger) != string(row.Trigger) || run.Spec.Actor != row.Actor {
		return fmt.Errorf("workflow run %s/%s identity does not match its retained ledger row", definition.Namespace, name)
	}
	if !workflow.Terminal(workflow.Phase(run.Status.Phase)) || run.Status.Phase != string(row.Phase) {
		return fmt.Errorf("workflow run %s/%s has no matching terminal observation", definition.Namespace, name)
	}
	err = client.Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &run.UID, ResourceVersion: &run.ResourceVersion}})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if live, readErr := client.Get(ctx, name, metav1.GetOptions{}); apierrors.IsNotFound(readErr) {
		return nil
	} else if readErr != nil {
		return readErr
	} else if live.GetUID() != run.UID {
		return fmt.Errorf("workflow resource was replaced during retention")
	}
	return controlplane.ErrObjectDeletionPending
}

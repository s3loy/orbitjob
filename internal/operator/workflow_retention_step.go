package operator

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

// RemoveWorkflowStep verifies the complete ledger identity before deleting a
// retained step. Object names contain only a digest prefix and are not proof
// that a live JobRun belongs to this workflow. UID/RV preconditions fence a
// replacement or state change between verification and deletion.
func (r RunRemover) RemoveWorkflowStep(ctx context.Context, definition revision.Identity, parent workflow.Run, row workflow.StepRun) error {
	if r.Client == nil {
		return fmt.Errorf("dynamic client is required")
	}
	if !workflow.Terminal(parent.Phase) || !jobrun.Terminal(row.Phase) || row.WorkflowRunID != parent.ID || row.Trigger != jobrun.Workflow || row.SourceUID != definition.SourceUID {
		return fmt.Errorf("refuse removal of an unowned or nonterminal workflow step %d", row.ID)
	}
	client := r.Client.Resource(jobRunGVR).Namespace(definition.Namespace)
	obj, found, err := v1alpha1.ResolveRun(ctx, client, v1alpha1.RunNameCandidates(definition.Name, row.OccurrenceKey),
		v1alpha1.RunIdentity{SourceUID: row.SourceUID, OccurrenceKey: row.OccurrenceKey, RevisionID: row.RevisionID, Trigger: v1alpha1.Trigger(row.Trigger)})
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	name := obj.GetName()
	var run v1alpha1.JobRun
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &run); err != nil {
		return err
	}
	if run.UID == "" || run.ResourceVersion == "" || run.DeletionTimestamp != nil {
		return fmt.Errorf("workflow step %s/%s has no stable deletion identity", definition.Namespace, name)
	}
	if run.Namespace != definition.Namespace || run.Name != name || run.Spec.ScheduledJobRef.UID != row.SourceUID || run.Spec.ScheduledJobRef.Name != definition.Name || run.Spec.DefinitionRevision != row.RevisionID || run.Spec.OccurrenceKey != row.OccurrenceKey || string(run.Spec.Trigger) != string(row.Trigger) {
		return fmt.Errorf("workflow step %s/%s differs from its retained ledger identity", definition.Namespace, name)
	}
	if !jobrun.Terminal(jobrun.Phase(run.Status.Phase)) || string(run.Status.Phase) != string(row.Phase) {
		return fmt.Errorf("workflow step %s/%s has no matching terminal observation", definition.Namespace, name)
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
		return fmt.Errorf("workflow step was replaced during retention")
	}
	return controlplane.ErrObjectDeletionPending
}

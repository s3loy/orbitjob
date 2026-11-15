package operator

import (
	"context"
	"fmt"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/workflow"
)

// ObserveWorkflowResource verifies the original, still-live execution instance
// before the walker starts steps, and reads cancellation directly from it.
// The ledger owns phase; the live resource owns the user's cancellation intent.
func (r Runtime) ObserveWorkflowResource(ctx context.Context, tenant string, row workflow.Run) (bool, error) {
	if r.Dynamic == nil || r.Revisions == nil {
		return false, fmt.Errorf("workflow resource observer is not configured")
	}
	if row.ResourceUID == "" {
		return false, fmt.Errorf("workflow resource publication is not bound yet")
	}
	rev, err := r.Revisions.RevisionByID(ctx, tenant, row.RevisionID)
	if err != nil {
		return false, err
	}
	if rev.Identity.SourceMode != workflow.SourceModeWorkflow || rev.Identity.SourceUID != row.SourceUID {
		return false, fmt.Errorf("workflow resource pinned revision identity mismatch")
	}
	obj, found, err := v1alpha1.ResolveRun(ctx, r.Dynamic.Resource(workflowRunGVR).Namespace(rev.Identity.Namespace),
		v1alpha1.RunNameCandidates(rev.Identity.Name, row.OccurrenceKey), v1alpha1.RunIdentity{
			SourceUID: row.SourceUID, OccurrenceKey: row.OccurrenceKey, RevisionID: row.RevisionID,
			Trigger: v1alpha1.Trigger(row.Trigger), Workflow: true,
		})
	if err != nil {
		return false, fmt.Errorf("observe original WorkflowRun: %w", err)
	}
	if !found {
		return false, fmt.Errorf("original WorkflowRun resource is unavailable")
	}
	if string(obj.GetUID()) != row.ResourceUID || obj.GetDeletionTimestamp() != nil {
		return false, fmt.Errorf("workflow resource instance was replaced or is deleting")
	}
	spec, err := workflowRunSpecFromUnstructured(*obj)
	if err != nil {
		return false, err
	}
	if spec.WorkflowRef.UID != row.SourceUID || spec.WorkflowRef.Name != rev.Identity.Name || spec.DefinitionRevision != row.RevisionID || spec.OccurrenceKey != row.OccurrenceKey || string(spec.Trigger) != string(row.Trigger) || spec.Actor != row.Actor {
		return false, fmt.Errorf("workflow resource spec differs from its recorded execution")
	}
	return spec.CancelRequested, nil
}

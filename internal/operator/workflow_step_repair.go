package operator

import (
	"context"
	"encoding/json"
	"fmt"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

// repairStepObjects republishes committed nonterminal steps from their pinned
// revisions. It runs independently of each referenced Job's cron/suspend state;
// manual-only definitions have no ordinary scheduling loop to repair them.
func (w WorkflowWalker) repairStepObjects(ctx context.Context, tenant, namespace string, parent workflow.Run, steps []workflow.StepRun, index map[string]revision.Revision) error {
	for _, step := range steps {
		if jobrun.Terminal(step.Phase) || step.Phase == jobrun.CancelUnknown {
			continue
		}
		if step.WorkflowRunID != parent.ID || step.Trigger != jobrun.Workflow {
			return fmt.Errorf("workflow step %d has invalid parent or origin", step.ID)
		}
		pinned, ok := index["uid:"+step.SourceUID]
		if !ok || pinned.ID != step.RevisionID {
			var err error
			pinned, err = w.Revisions.RevisionByID(ctx, tenant, step.RevisionID)
			if err != nil {
				return fmt.Errorf("read pinned step revision %d: %w", step.RevisionID, err)
			}
		}
		if pinned.Identity.SourceUID != step.SourceUID || pinned.Identity.SourceMode != "kubernetes" || pinned.Identity.Namespace != namespace {
			return fmt.Errorf("workflow step %d pinned source mismatch", step.ID)
		}
		var declared v1alpha1.ScheduledJobSpec
		if err := json.Unmarshal([]byte(pinned.NormalizedSpec), &declared); err != nil {
			return err
		}
		obj := w.stepObject(pinned, declared, parent, step.OccurrenceKey)
		// Recovery must survive a deleted source. The ledger is the durable
		// identity; a dangling source owner would restart a GC/repair loop.
		obj.OwnerReferences = nil
		obj.Spec.CancelRequested = parent.Phase == workflow.PhaseCancelRequested || parent.Phase == workflow.PhaseCancelUnknown || step.Phase == jobrun.CancelRequested
		if err := w.Publisher.Publish(ctx, obj); err != nil {
			return fmt.Errorf("repair workflow step %d: %w", step.ID, err)
		}
	}
	return nil
}

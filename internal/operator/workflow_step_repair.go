package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

// repairStepObjects republishes committed nonterminal steps from their pinned
// revisions. It runs independently of each referenced Job's cron/suspend state;
// manual-only definitions have no ordinary scheduling loop to repair them.
func (w WorkflowWalker) repairStepObjects(ctx context.Context, tenant, namespace string, parent workflow.Run, steps []workflow.StepRun, index map[string]revision.Revision, isStopping func() bool) error {
	var failures []error
	for _, step := range steps {
		if err := w.repairStepObject(ctx, tenant, namespace, parent, step, index, isStopping); err != nil {
			failures = append(failures, fmt.Errorf("repair workflow step %d: %w", step.ID, err))
		}
	}
	return errors.Join(failures...)
}

func (w WorkflowWalker) repairStepObject(ctx context.Context, tenant, namespace string, parent workflow.Run, step workflow.StepRun, index map[string]revision.Revision, isStopping func() bool) error {
	if jobrun.Terminal(step.Phase) || step.Phase == jobrun.CancelUnknown {
		return nil
	}
	pinned, err := w.stepRevision(ctx, tenant, namespace, parent, step, index)
	if err != nil {
		return err
	}
	var declared v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(pinned.NormalizedSpec), &declared); err != nil {
		return fmt.Errorf("decode pinned step revision %d: %w", pinned.ID, err)
	}
	obj := w.stepObject(pinned, declared, parent, step.OccurrenceKey)
	// Recovery must survive a deleted source. The ledger is the durable
	// identity; a dangling source owner would restart a GC/repair loop.
	obj.OwnerReferences = nil
	// Sample the workflow's stop intent immediately before publishing the CR.
	obj.Spec.CancelRequested = isStopping() || step.Phase == jobrun.CancelRequested
	return w.Publisher.Publish(ctx, obj)
}

func (w WorkflowWalker) stepRevision(ctx context.Context, tenant, namespace string, parent workflow.Run, step workflow.StepRun, index map[string]revision.Revision) (revision.Revision, error) {
	if step.WorkflowRunID != parent.ID || step.Trigger != jobrun.Workflow {
		return revision.Revision{}, fmt.Errorf("workflow step %d has invalid parent or origin", step.ID)
	}
	pinned, ok := index["uid:"+step.SourceUID]
	if !ok || pinned.ID != step.RevisionID {
		var err error
		pinned, err = w.Revisions.RevisionByID(ctx, tenant, step.RevisionID)
		if err != nil {
			return revision.Revision{}, fmt.Errorf("read pinned step revision %d: %w", step.RevisionID, err)
		}
	}
	if pinned.ID != step.RevisionID || pinned.Identity.SourceUID != step.SourceUID || pinned.Identity.SourceMode != "kubernetes" || pinned.Identity.Namespace != namespace {
		return revision.Revision{}, fmt.Errorf("workflow step %d pinned source mismatch", step.ID)
	}
	return pinned, nil
}

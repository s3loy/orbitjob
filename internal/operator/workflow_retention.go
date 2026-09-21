package operator

import (
	"context"
	"encoding/json"
	"fmt"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

// DefaultWorkflowHistory mirrors the run retention defaults: zero in a spec
// cannot mean "keep nothing", so an unset policy resolves to these values.
const (
	DefaultWorkflowSuccessfulHistory = 3
	DefaultWorkflowFailedHistory     = 3
)

// WorkflowRetentionDefinitions is the revision access retention needs, and
// nothing else: ActiveWorkflowRevisions is the sweep's input -- the tenant's
// workflow-sourced revisions, the only listing that returns them -- and
// ActiveRevisions is the full active list a step run's source uid resolves
// against, because a step is an ordinary run of a ScheduledJob or check
// definition, not of the workflow that ordered it.
type WorkflowRetentionDefinitions interface {
	ActiveWorkflowRevisions(ctx context.Context, tenantID string) ([]revision.Revision, error)
	ActiveRevisions(ctx context.Context, tenantID string) ([]revision.Revision, error)
}

// WorkflowRetainer trims workflow history: a workflow definition's terminal
// runs beyond its retained counts are pruned steps-then-row, and each pruned
// run's step CRs are removed before the ledger rows are.
//
// The Kubernetes objects go first, the store's atomic delete second — the
// same order the job-run retainer uses. A step CR removed while its row
// survives costs a retry-free no-op next sweep; a workflow row deleted while
// its CR survived would strand an object no ledger row explains. The store's
// DeleteRun removes the run's step rows and the row in one transaction, so
// the RESTRICT foreign key makes an out-of-order delete fail loudly instead
// of orphaning steps.
type WorkflowRetainer struct {
	Definitions WorkflowRetentionDefinitions
	Workflows   WorkflowRunStore
	History     WorkflowHistoryPruner
	Remover     RunRemover
	Tenants     []string
}

// Sweep prunes history for every active workflow definition and reports how
// many workflow runs it removed.
func (r WorkflowRetainer) Sweep(ctx context.Context) (int, error) {
	if r.Definitions == nil || r.Workflows == nil || r.History == nil || r.Remover.Client == nil {
		return 0, fmt.Errorf("workflow retainer dependencies are required")
	}
	removed := 0
	for _, tenant := range r.Tenants {
		defs, err := r.Definitions.ActiveWorkflowRevisions(ctx, tenant)
		if err != nil {
			return removed, fmt.Errorf("list active workflow revisions for tenant %s: %w", tenant, err)
		}
		for _, rev := range defs {
			count, err := r.pruneDefinition(ctx, tenant, rev)
			if err != nil {
				return removed, err
			}
			removed += count
		}
	}
	return removed, nil
}

func (r WorkflowRetainer) pruneDefinition(ctx context.Context, tenant string, rev revision.Revision) (int, error) {
	var spec v1alpha1.WorkflowJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &spec); err != nil {
		return 0, fmt.Errorf("decode revision %d for %s: %w", rev.ID, rev.Identity.Name, err)
	}
	keepSuccessful, keepFailed := workflowHistoryLimits(spec.History)

	prunable, err := r.History.PrunableRuns(ctx, tenant, rev.Identity.SourceUID, keepSuccessful, keepFailed)
	if err != nil {
		return 0, fmt.Errorf("scan prunable workflow runs for %s: %w", rev.Identity.Name, err)
	}

	removed := 0
	for _, run := range prunable {
		if err := r.removeStepObjects(ctx, tenant, run); err != nil {
			return removed, err
		}
		deleted, err := r.History.DeleteRun(ctx, tenant, run.ID)
		if err != nil {
			return removed, fmt.Errorf("delete workflow run %d: %w", run.ID, err)
		}
		if deleted {
			removed++
		}
	}
	return removed, nil
}

// removeStepObjects deletes the JobRun CRs of a workflow run's steps ahead of
// the atomic row delete. Steps run ordinary definitions, so a step CR is
// named and addressed exactly like any other run; a step whose definition was
// deleted is skipped, because the owner reference already garbage-collected
// its CR when the definition died.
func (r WorkflowRetainer) removeStepObjects(ctx context.Context, tenant string, run workflow.Run) error {
	steps, err := r.Workflows.Steps(ctx, tenant, run.ID)
	if err != nil {
		return fmt.Errorf("list steps of workflow run %d: %w", run.ID, err)
	}
	if len(steps) == 0 {
		return nil
	}
	defs, err := r.Definitions.ActiveRevisions(ctx, tenant)
	if err != nil {
		return fmt.Errorf("list active revisions for tenant %s: %w", tenant, err)
	}
	index := definitionIndex(defs)
	for _, step := range steps {
		def, ok := index["uid:"+step.SourceUID]
		if !ok {
			continue
		}
		if err := r.Remover.Remove(ctx, def.Identity.Namespace, def.Identity.Name, step.OccurrenceKey); err != nil {
			return fmt.Errorf("remove step run of workflow run %d: %w", run.ID, err)
		}
	}
	return nil
}

// workflowHistoryLimits resolves a workflow definition's retention into
// concrete counts. A negative value is treated as unset rather than as "keep
// nothing", so a malformed spec cannot silently delete a tenant's entire
// workflow history.
func workflowHistoryLimits(policy v1alpha1.HistoryPolicy) (successful, failed int) {
	successful = int(policy.SuccessfulRuns)
	if successful <= 0 {
		successful = DefaultWorkflowSuccessfulHistory
	}
	failed = int(policy.FailedRuns)
	if failed <= 0 {
		failed = DefaultWorkflowFailedHistory
	}
	return successful, failed
}

package projection

import (
	"context"
	"encoding/json"
	"fmt"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

// ApplyWorkflowJob materializes a WorkflowJob custom resource as an immutable
// definition revision, the same store path a ScheduledJob CR projects through.
// The revision is keyed source_mode=workflow, source_uid=the CR's UID,
// generation=metadata.generation, so re-projecting a generation is idempotent
// and a spec edit makes a new revision while in-flight runs keep the one they
// were pinned to.
//
// The normalized form is the full WorkflowJobSpec, not a hand-picked subset:
// the workflow walker decodes a run's pinned revision into exactly this shape
// to walk the task DAG, so anything omitted here would be missing at
// execution time. Marshal is deterministic for a fixed struct definition,
// which is what makes the spec hash meaningful.
//
// A WorkflowJob revision has no resource group: the CR surface refuses
// group-scoped keys, so no CR belongs to a tier.
func (s Service) ApplyWorkflowJob(ctx context.Context, obj v1alpha1.WorkflowJob, tenantID, actor string) (int64, error) {
	if tenantID == "" {
		return 0, fmt.Errorf("tenant is required")
	}
	if s.Revisions == nil {
		return 0, fmt.Errorf("revision writer is required")
	}
	// Only the fields this projection is responsible for. Identity and
	// generation are validated by revision.New, so they are not re-checked
	// here where the two could drift apart.
	if obj.Spec.Schedule == "" || len(obj.Spec.Tasks) == 0 {
		return 0, fmt.Errorf("workflow %s/%s: schedule and tasks are required", obj.GetNamespace(), obj.GetName())
	}

	rev, err := revision.New(
		revision.Identity{
			SourceMode: workflow.SourceModeWorkflow,
			SourceUID:  string(obj.UID),
			Namespace:  obj.Namespace,
			Name:       obj.Name,
		},
		obj.Generation,
		normalizeWorkflowSpec(obj.Spec),
		actor,
		"",
		// A revision is created now, not when the CR was created: re-projecting
		// an old resource must not backdate the revision it produces.
		s.now(),
	)
	if err != nil {
		return 0, err
	}
	return s.Revisions.ApplyRevisionForTenant(ctx, tenantID, rev)
}

// normalizeWorkflowSpec renders the task DAG deterministically. Marshalling a
// struct of strings, integers and slices cannot fail, so this returns no error
// rather than forcing every caller to handle an impossible branch.
func normalizeWorkflowSpec(spec v1alpha1.WorkflowJobSpec) string {
	raw, _ := json.Marshal(spec)
	return string(raw)
}

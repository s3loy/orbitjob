package operator

import (
	"context"
	"encoding/json"
	"fmt"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

// snapshotDefinitions never substitutes an active revision when accepted
// history is unavailable. The run remains blocked and observable for repair.
func (w WorkflowWalker) snapshotDefinitions(ctx context.Context, tenant string, run workflow.Run, dag workflow.DAG, namespace string) (map[string]revision.Revision, error) {
	if run.SnapshotVersion != 1 || len(run.TaskSnapshots) != len(dag.Tasks) {
		return nil, fmt.Errorf("workflow run %d has an incomplete or unsupported snapshot", run.ID)
	}
	index := make(map[string]revision.Revision, len(dag.Tasks)*2)
	for _, task := range dag.Tasks {
		s, ok := run.TaskSnapshots[task.Name]
		if !ok || s.Namespace != namespace || s.Name != task.JobRef || s.SourceUID == "" || s.RevisionID <= 0 {
			return nil, fmt.Errorf("workflow run %d task %s snapshot identity mismatch", run.ID, task.Name)
		}
		pinned, err := w.Revisions.RevisionByID(ctx, tenant, s.RevisionID)
		if err != nil {
			return nil, fmt.Errorf("workflow run %d task %s snapshot revision unavailable: %w", run.ID, task.Name, err)
		}
		if pinned.Identity.SourceMode != "kubernetes" || pinned.Identity.SourceUID != s.SourceUID || pinned.Identity.Namespace != namespace || pinned.Identity.Name != s.Name {
			return nil, fmt.Errorf("workflow run %d task %s pinned source mismatch", run.ID, task.Name)
		}
		var spec v1alpha1.ScheduledJobSpec
		if err := json.Unmarshal([]byte(pinned.NormalizedSpec), &spec); err != nil {
			return nil, err
		}
		if s.Image == "" || spec.JobTemplate.Image != s.Image {
			return nil, fmt.Errorf("workflow run %d task %s pinned image mismatch", run.ID, task.Name)
		}
		index["name:"+s.Name] = pinned
		index["uid:"+s.SourceUID] = pinned
	}
	return index, nil
}

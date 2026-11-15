package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"

	"github.com/lib/pq"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/executionpolicy"
)

var immutableWorkflowImage = regexp.MustCompile(`^[^[:space:]@]+@sha256:[a-f0-9]{64}$`)

const workflowSnapshotCandidatesSQL = `SELECT source_uid,id,source_namespace,source_name,normalized_spec::text
FROM job_definition_revisions WHERE tenant_id=$1 AND source_mode='kubernetes'
AND source_namespace=$2 AND source_name=ANY($3) AND is_active ORDER BY source_name,source_uid`

// prepareWorkflowSnapshot reads the whole reference set with one statement
// snapshot. It does not claim the Kubernetes objects were observed atomically.
// Admission already holds the workflow source lock; references are immutable
// revisions whose active pointer may advance immediately after this read.
func prepareWorkflowSnapshot(ctx context.Context, tx *sql.Tx, tenant string, run workflow.Run) (int, map[string]workflow.TaskSnapshot, error) {
	var namespace, raw string
	if err := tx.QueryRowContext(ctx, `SELECT source_namespace,normalized_spec::text FROM job_definition_revisions WHERE id=$1 AND tenant_id=$2`, run.RevisionID, tenant).Scan(&namespace, &raw); err != nil {
		return 0, nil, err
	}
	var spec v1alpha1.WorkflowJobSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return 0, nil, fmt.Errorf("decode pinned workflow snapshot policy: %w", err)
	}
	if spec.ExecutionPolicyVersion == 0 {
		return 0, nil, nil
	}
	if err := spec.Validate(); err != nil {
		return 0, nil, err
	}
	if err := workflow.ValidateDAG(workflow.DAGFromSpec(spec)); err != nil {
		return 0, nil, err
	}
	unique := make(map[string]bool, len(spec.Tasks))
	names := make([]string, 0, len(spec.Tasks))
	for _, task := range spec.Tasks {
		if !unique[task.JobRef.Name] {
			names = append(names, task.JobRef.Name)
			unique[task.JobRef.Name] = true
		}
	}
	rows, err := tx.QueryContext(ctx, workflowSnapshotCandidatesSQL, tenant, namespace, pq.Array(names))
	if err != nil {
		return 0, nil, err
	}
	byName := make(map[string]workflow.TaskSnapshot, len(names))
	for rows.Next() {
		var s workflow.TaskSnapshot
		var raw string
		if err := rows.Scan(&s.SourceUID, &s.RevisionID, &s.Namespace, &s.Name, &raw); err != nil {
			_ = rows.Close()
			return 0, nil, err
		}
		if _, duplicate := byName[s.Name]; duplicate {
			_ = rows.Close()
			return 0, nil, &jobrun.AdmissionBlockedError{Reason: "workflow_reference_ambiguous"}
		}
		var job v1alpha1.ScheduledJobSpec
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			_ = rows.Close()
			return 0, nil, fmt.Errorf("decode workflow reference %s: %w", s.Name, err)
		}
		if executionpolicy.ValidateResolved(job.ExecutionPolicyVersion, job.RetrySafe, job.JobTemplate.BackoffLimit, job.RetryPolicy.MaxAttempts, job.JobTemplate.Resources) != nil || !immutableWorkflowImage.MatchString(job.JobTemplate.Image) {
			_ = rows.Close()
			return 0, nil, &jobrun.AdmissionBlockedError{Reason: "workflow_reference_not_ready"}
		}
		s.Image = job.JobTemplate.Image
		byName[s.Name] = s
	}
	scanErr := rows.Err()
	closeErr := rows.Close()
	if scanErr != nil {
		return 0, nil, scanErr
	}
	if closeErr != nil {
		return 0, nil, closeErr
	}
	if len(byName) != len(names) {
		return 0, nil, &jobrun.AdmissionBlockedError{Reason: "workflow_reference_not_ready"}
	}
	out := make(map[string]workflow.TaskSnapshot, len(spec.Tasks))
	for _, task := range spec.Tasks {
		out[task.Name] = byName[task.JobRef.Name]
	}
	return 1, out, nil
}

func insertWorkflowSnapshots(ctx context.Context, tx *sql.Tx, tenant string, id int64, run workflow.Run, snapshots map[string]workflow.TaskSnapshot) error {
	names := make([]string, 0, len(snapshots))
	for task := range snapshots {
		names = append(names, task)
	}
	sort.Strings(names)
	for _, task := range names {
		s := snapshots[task]
		if _, err := tx.ExecContext(ctx, `INSERT INTO workflow_task_snapshots(tenant_id,workflow_run_id,task_name,source_uid,revision_id,occurrence_key) VALUES($1,$2,$3,$4,$5,$6)`, tenant, id, task, s.SourceUID, s.RevisionID, workflow.StepOccurrenceKey(run.SourceUID, run.OccurrenceKey, task)); err != nil {
			return err
		}
	}
	return nil
}

// A deleted definition cannot rewrite an already accepted graph. An inactive
// source still blocks new parent admission; only a verified snapshot permits
// this run's original step identity to use the pinned concurrency policy.
func checkWorkflowStepAdmission(ctx context.Context, tx *sql.Tx, tenant string, parent int64, mode string, step workflow.StepRun) error {
	err := checkSourceAdmission(ctx, tx, tenant, mode, step.SourceUID, jobrun.Workflow)
	var blocked *jobrun.AdmissionBlockedError
	if !errors.As(err, &blocked) || (blocked.Reason != "inactive_source" && blocked.Reason != "source_deleted") {
		return err
	}
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT r.normalized_spec::text FROM workflow_task_snapshots s
JOIN workflow_run_control_plane w ON w.id=s.workflow_run_id AND w.tenant_id=s.tenant_id
JOIN job_definition_revisions r ON r.id=s.revision_id AND r.tenant_id=s.tenant_id
WHERE s.tenant_id=$1 AND s.workflow_run_id=$2 AND w.snapshot_version=1
AND s.source_uid=$3 AND s.revision_id=$4 AND s.occurrence_key=$5`, tenant, parent, step.SourceUID, step.RevisionID, step.OccurrenceKey).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return blocked
		}
		return err
	}
	var spec v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return err
	}
	if spec.ConcurrencyPolicy != v1alpha1.Forbid && spec.ConcurrencyPolicy != v1alpha1.Replace {
		return nil
	}
	var count int
	if err := tx.QueryRowContext(ctx, countOpenRunsSQL, tenant, step.SourceUID, terminalPhases()).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return &jobrun.AdmissionBlockedError{Policy: string(spec.ConcurrencyPolicy), Reason: "concurrency"}
	}
	return nil
}

// SnapshotWorkflowStep identifies an already accepted task obligation. It is
// not a new admission grant: CreateAttempt still locks and checks the parent.
func (r *ControlPlaneRepository) SnapshotWorkflowStep(ctx context.Context, tenant string, runID, revisionID int64) (bool, error) {
	var found bool
	err := r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM job_run_control_plane j
JOIN workflow_run_control_plane w ON w.id=j.workflow_run_id AND w.tenant_id=j.tenant_id
JOIN workflow_task_snapshots s ON s.workflow_run_id=w.id AND s.tenant_id=w.tenant_id
AND s.source_uid=j.source_uid AND s.revision_id=j.revision_id AND s.occurrence_key=j.occurrence_key
WHERE j.id=$1 AND j.tenant_id=$2 AND j.revision_id=$3 AND j.trigger='Workflow' AND w.snapshot_version=1)`, runID, tenant, revisionID).Scan(&found)
	})
	return found, err
}

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"orbitjob/internal/core/domain/jobrun"
)

var recordedAttemptColumns = strings.ReplaceAll(executionColumns, "a.", "current_attempt.") + `,
 a.id,a.attempt_number,a.phase,a.kubernetes_job_name,COALESCE(a.kubernetes_job_uid,''),COALESCE(a.owner_run_uid,''),
 a.execution_unknown_since,COALESCE(a.execution_unknown_reason,''),a.termination_evidence`

const recordedAttemptFrom = ` FROM job_run_control_plane r
 JOIN job_run_attempts_control_plane current_attempt ON current_attempt.run_id=r.id AND current_attempt.tenant_id=r.tenant_id AND current_attempt.attempt_number=r.attempt
 JOIN job_run_attempts_control_plane a ON a.run_id=r.id AND a.tenant_id=r.tenant_id
 JOIN job_definition_revisions v ON v.id=r.revision_id AND v.tenant_id=r.tenant_id `

// Match the domain checkpoint rule, including old deletion-only "done" proofs.
const pendingEvidenceSQL = `COALESCE(a.termination_evidence->>'protocol','')='pod-hold-v1'
 AND (COALESCE(a.termination_evidence->>'hold_release','')<>'done'
 OR COALESCE(a.termination_evidence->>'revoked','false')<>'false'
 OR COALESCE(a.termination_evidence->>'stop_boundary','') NOT IN ('failure_target','success_criteria_met','failed','complete'))`

func scanRecordedAttempt(row interface{ Scan(...any) error }) (jobrun.RecordedAttempt, error) {
	var target jobrun.RecordedAttempt
	e := &target.Execution
	var at, since, targetSince sql.NullTime
	err := row.Scan(&e.Run.ID, &e.Run.RevisionID, &e.Run.Phase, &e.Run.Attempt, &e.Run.MaxAttempts, &at,
		&e.Run.AttemptUID, &e.Run.AttemptJobName, &e.Run.AttemptPhase, &e.Run.OwnerRunUID,
		&since, &e.Run.UnknownReason, &e.Run.TerminationEvidence,
		&e.SourceUID, &e.OccurrenceKey, &e.Namespace, &e.DefinitionName, &e.SourceMode, &e.Actor, &e.Trigger, &e.PhaseUpdatedAt, &e.DispatchStage,
		&target.ID, &target.Number, &target.Phase, &target.JobName, &target.JobUID, &target.OwnerRunUID,
		&targetSince, &target.UnknownReason, &target.TerminationEvidence)
	if at.Valid {
		e.Run.ScheduledFor = at.Time
	}
	if since.Valid {
		e.Run.UnknownSince = &since.Time
	}
	if targetSince.Valid {
		target.UnknownSince = &targetSince.Time
	}
	return target, err
}

func (r *ControlPlaneRepository) RecordedExecution(ctx context.Context, tenant, namespace, name, uid string) (jobrun.RecordedAttempt, bool, error) {
	var target jobrun.RecordedAttempt
	if namespace == "" || name == "" || uid == "" {
		return target, false, fmt.Errorf("recorded execution identity is required")
	}
	err := r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		target, err = scanRecordedAttempt(tx.QueryRowContext(ctx, `SELECT `+recordedAttemptColumns+recordedAttemptFrom+`
 WHERE r.tenant_id=$1 AND v.source_namespace=$2 AND a.kubernetes_job_name=$3 AND a.kubernetes_job_uid=$4`, tenant, namespace, name, uid))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return target, false, nil
	}
	return target, err == nil, err
}

// PendingEvidenceAfter includes terminal runs and superseded attempts. Its key
// is the target attempt row, independent of current-run recovery's cursor.
func (r *ControlPlaneRepository) PendingEvidenceAfter(ctx context.Context, tenant string, after int64, limit int) ([]jobrun.RecordedAttempt, error) {
	if limit < 1 || limit > 1000 || after < 0 {
		return nil, fmt.Errorf("evidence recovery requires a nonnegative cursor and limit 1..1000")
	}
	var out []jobrun.RecordedAttempt
	err := r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+recordedAttemptColumns+recordedAttemptFrom+`
 WHERE r.tenant_id=$1 AND a.id>$2 AND `+pendingEvidenceSQL+` ORDER BY a.id LIMIT $3`, tenant, after, limit)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			target, err := scanRecordedAttempt(rows)
			if err != nil {
				return err
			}
			out = append(out, target)
		}
		return rows.Err()
	})
	return out, err
}

// Always lock parent, logical run, then the specific target attempt. Historical
// cleanup may proceed after the run advances, but cannot mutate its new attempt.
func lockRecordedAttempt(ctx context.Context, tx *sql.Tx, tenant string, want jobrun.RecordedAttempt) (jobrun.RecordedAttempt, sql.NullInt64, error) {
	parent, err := lockWorkflowParentForObservation(ctx, tx, tenant, want.Execution.Run.ID)
	if err != nil {
		return jobrun.RecordedAttempt{}, parent, err
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM job_run_control_plane WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, want.Execution.Run.ID).Scan(&id); err != nil {
		return jobrun.RecordedAttempt{}, parent, err
	}
	got, err := scanRecordedAttempt(tx.QueryRowContext(ctx, `SELECT `+recordedAttemptColumns+recordedAttemptFrom+`
 WHERE r.tenant_id=$1 AND r.id=$2 AND a.id=$3 FOR UPDATE OF a`, tenant, id, want.ID))
	if err != nil {
		return got, parent, err
	}
	if got.Number != want.Number || got.JobUID != want.JobUID || got.JobName != want.JobName ||
		got.OwnerRunUID != want.OwnerRunUID || got.Execution.Namespace != want.Execution.Namespace || got.JobUID == "" {
		return got, parent, ErrAttemptOwnership
	}
	return got, parent, nil
}

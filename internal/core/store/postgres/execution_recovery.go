package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"orbitjob/internal/core/domain/jobrun"
)

const executionColumns = `r.id,r.revision_id,r.phase,r.attempt,r.max_attempts,r.scheduled_for,
 COALESCE(a.kubernetes_job_uid,''),a.kubernetes_job_name,a.phase,COALESCE(a.owner_run_uid,''),
 a.execution_unknown_since,COALESCE(a.execution_unknown_reason,''),a.termination_evidence,
 r.source_uid,rtrim(r.occurrence_key),v.source_namespace,v.source_name,v.source_mode,r.actor,r.trigger,r.updated_at,a.dispatch_stage`
const executionFrom = ` FROM job_run_control_plane r
 JOIN job_run_attempts_control_plane a ON a.run_id=r.id AND a.tenant_id=r.tenant_id AND a.attempt_number=r.attempt
 JOIN job_definition_revisions v ON v.id=r.revision_id AND v.tenant_id=r.tenant_id `

func scanExecution(row interface{ Scan(...any) error }) (jobrun.Execution, error) {
	var e jobrun.Execution
	var at, since sql.NullTime
	err := row.Scan(&e.Run.ID, &e.Run.RevisionID, &e.Run.Phase, &e.Run.Attempt, &e.Run.MaxAttempts, &at,
		&e.Run.AttemptUID, &e.Run.AttemptJobName, &e.Run.AttemptPhase, &e.Run.OwnerRunUID,
		&since, &e.Run.UnknownReason, &e.Run.TerminationEvidence,
		&e.SourceUID, &e.OccurrenceKey, &e.Namespace, &e.DefinitionName, &e.SourceMode, &e.Actor, &e.Trigger, &e.PhaseUpdatedAt, &e.DispatchStage)
	if at.Valid {
		e.Run.ScheduledFor = at.Time
	}
	if since.Valid {
		e.Run.UnknownSince = &since.Time
	}
	return e, err
}

func (r *ControlPlaneRepository) CurrentExecution(ctx context.Context, tenant string, id int64) (jobrun.Execution, bool, error) {
	var e jobrun.Execution
	err := r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		e, err = scanExecution(tx.QueryRowContext(ctx, `SELECT `+executionColumns+executionFrom+` WHERE r.tenant_id=$1 AND r.id=$2`, tenant, id))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return e, false, nil
	}
	return e, err == nil, err
}

// Isolate an owned Job from the old create-before-reservation crash window.
// There is no attempt row to carry proof, so API deletion cannot release it.
func (r *ControlPlaneRepository) MarkUnrecordedExecutionUnknown(ctx context.Context, tenant string, id int64, jobUID string) error {
	return r.markUnrecordedExecutionUnknown(ctx, tenant, id, jobUID, true)
}

// A conflicting deterministic name proves an evidence gap, not ownership. Its
// UID is diagnostic only and must never authorize a later deletion retry.
func (r *ControlPlaneRepository) MarkConflictingUnrecordedExecutionUnknown(ctx context.Context, tenant string, id int64, jobUID string) error {
	return r.markUnrecordedExecutionUnknown(ctx, tenant, id, jobUID, false)
}

func (r *ControlPlaneRepository) markUnrecordedExecutionUnknown(ctx context.Context, tenant string, id int64, jobUID string, trusted bool) error {
	if jobUID == "" {
		return fmt.Errorf("legacy job UID is required")
	}
	return r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		parent, err := lockWorkflowParentForObservation(ctx, tx, tenant, id)
		if err != nil {
			return err
		}
		var phase jobrun.Phase
		var attempt int
		var actor string
		if err := tx.QueryRowContext(ctx, `SELECT phase,attempt,actor FROM job_run_control_plane WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&phase, &attempt, &actor); err != nil {
			return err
		}
		if attempt != 0 || jobrun.Terminal(phase) {
			return ErrRunChanged
		}
		if phase == jobrun.CancelUnknown {
			if !trusted {
				return isolateWorkflowForUnknown(ctx, tx, tenant, parent, r.now().UTC())
			}
			var original string
			if err := tx.QueryRowContext(ctx, `SELECT diff->>'job_uid' FROM audit_events WHERE tenant_id=$1 AND resource_type=$2 AND resource_id=$3 AND event_type='execution.legacy_unknown' ORDER BY created_at DESC,id DESC LIMIT 1`, tenant, auditResourceRun, strconv.FormatInt(id, 10)).Scan(&original); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ErrAttemptOwnership
				}
				return err
			}
			if original != jobUID {
				return ErrAttemptOwnership
			}
			return isolateWorkflowForUnknown(ctx, tx, tenant, parent, r.now().UTC())
		}
		if phase != jobrun.Pending && phase != jobrun.CancelRequested {
			return ErrRunChanged
		}
		if _, err := tx.ExecContext(ctx, `UPDATE job_run_control_plane SET phase='CancelUnknown',updated_at=$1 WHERE tenant_id=$2 AND id=$3`, r.now().UTC(), tenant, id); err != nil {
			return err
		}
		if err := isolateWorkflowForUnknown(ctx, tx, tenant, parent, r.now().UTC()); err != nil {
			return err
		}
		event := "execution.legacy_unknown"
		if !trusted {
			event = "execution.legacy_conflict"
		}
		return insertControlPlaneAudit(ctx, tx, tenant, actor, event, auditResourceRun, strconv.FormatInt(id, 10), map[string]any{"job_uid": jobUID, "attempt": 0, "reason": "unrecorded_execution"})
	})
}

func (r *ControlPlaneRepository) ExecutionRecoveriesAfter(ctx context.Context, tenant string, after int64, limit int) ([]jobrun.Execution, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("execution recovery limit must be 1..1000")
	}
	var out []jobrun.Execution
	err := r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+executionColumns+executionFrom+`
 WHERE r.tenant_id=$1 AND r.id>$2 AND r.attempt>0
 AND r.phase IN ('CreatingAttempt','Running','ExecutionUnknown','CancelRequested','CancelUnknown') ORDER BY r.id LIMIT $3`, tenant, after, limit)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			e, err := scanExecution(rows)
			if err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// Lock in parent/run/attempt order, then reject a stale snapshot. Observations
// never replace a newer attempt, trusted terminal fact, or committed stop.
func lockRecovery(ctx context.Context, tx *sql.Tx, tenant string, want jobrun.Execution) (jobrun.Execution, error) {
	if _, err := lockWorkflowParentForObservation(ctx, tx, tenant, want.Run.ID); err != nil {
		return jobrun.Execution{}, err
	}
	got, err := scanExecution(tx.QueryRowContext(ctx, `SELECT `+executionColumns+executionFrom+` WHERE r.tenant_id=$1 AND r.id=$2 FOR UPDATE OF r,a`, tenant, want.Run.ID))
	if err != nil {
		return got, err
	}
	if got.Run.Attempt != want.Run.Attempt || got.Run.AttemptUID != want.Run.AttemptUID || got.Run.AttemptJobName != want.Run.AttemptJobName || jobrun.Terminal(got.Run.Phase) {
		return got, ErrRunChanged
	}
	return got, nil
}

// CancelPreparedAttempt proves that no dispatcher crossed the durable send
// boundary. The same parent/run/attempt locks serialize it with marker commit.
func (r *ControlPlaneRepository) CancelPreparedAttempt(ctx context.Context, tenant string, want jobrun.Execution) (bool, error) {
	changed := false
	err := r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		got, err := lockRecovery(ctx, tx, tenant, want)
		if err != nil {
			return err
		}
		if got.DispatchStage != jobrun.DispatchPrepared || got.Run.AttemptUID != "" || got.Run.UnknownSince != nil ||
			(got.Run.Phase != jobrun.CancelRequested && got.Run.Phase != jobrun.CancelUnknown) {
			return ErrRunChanged
		}
		at := r.now().UTC()
		if _, err = tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET phase='Canceled',completed_at=COALESCE(completed_at,$1) WHERE tenant_id=$2 AND run_id=$3 AND attempt_number=$4`, at, tenant, got.Run.ID, got.Run.Attempt); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE job_run_control_plane SET phase='Canceled',updated_at=$1 WHERE tenant_id=$2 AND id=$3`, at, tenant, got.Run.ID); err != nil {
			return err
		}
		if err = insertControlPlaneAudit(ctx, tx, tenant, got.Actor, auditEventRunStatusChanged, auditResourceRun, strconv.FormatInt(got.Run.ID, 10), map[string]any{"from": got.Run.Phase, "to": jobrun.Canceled, "attempt_number": got.Run.Attempt, "dispatch_stage": jobrun.DispatchPrepared, "never_dispatched": true}); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed && err == nil, err
}

func (r *ControlPlaneRepository) MarkExecutionUnknown(ctx context.Context, tenant string, want jobrun.Execution, reason string) error {
	if reason != jobrun.ReasonJobNotFound && reason != jobrun.ReasonJobUIDMismatch && reason != jobrun.ReasonPodIdentityAmbiguous {
		return fmt.Errorf("invalid execution uncertainty reason")
	}
	return r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		got, err := lockRecovery(ctx, tx, tenant, want)
		if err != nil {
			return err
		}
		if (got.Run.AttemptUID == "" && got.DispatchStage != jobrun.DispatchMayHaveStarted) || jobrun.Terminal(got.Run.AttemptPhase) || got.Run.AttemptPhase == jobrun.RetryWaiting {
			return ErrRunChanged
		}
		var parent sql.NullInt64
		if err = tx.QueryRowContext(ctx, `SELECT workflow_run_id FROM job_run_control_plane WHERE tenant_id=$1 AND id=$2`, tenant, got.Run.ID).Scan(&parent); err != nil {
			return err
		}
		if err = isolateWorkflowForUnknown(ctx, tx, tenant, parent, r.now().UTC()); err != nil {
			return err
		}
		if got.Run.UnknownSince != nil && got.Run.UnknownReason == reason {
			return nil
		}
		if _, err = tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET execution_unknown_since=COALESCE(execution_unknown_since,$1),execution_unknown_reason=$2,phase='ExecutionUnknown' WHERE tenant_id=$3 AND run_id=$4 AND attempt_number=$5`, r.now().UTC(), reason, tenant, got.Run.ID, got.Run.Attempt); err != nil {
			return err
		}
		canceling := got.Run.Phase == jobrun.CancelRequested || got.Run.Phase == jobrun.CancelUnknown
		if !canceling {
			if _, err = tx.ExecContext(ctx, `UPDATE job_run_control_plane SET phase='ExecutionUnknown',updated_at=$1 WHERE tenant_id=$2 AND id=$3`, r.now().UTC(), tenant, got.Run.ID); err != nil {
				return err
			}
		}
		return insertControlPlaneAudit(ctx, tx, tenant, got.Actor, "execution.unknown", auditResourceRun, strconv.FormatInt(got.Run.ID, 10), map[string]any{"attempt": got.Run.Attempt, "job_uid": got.Run.AttemptUID, "reason": reason})
	})
}

// RecordTerminationEvidence accepts only internal, identity-checked observations.
// It does not change a business outcome or turn API absence into a stop fact.
func (r *ControlPlaneRepository) RecordTerminationEvidence(ctx context.Context, tenant string, want jobrun.Execution, evidence []byte) error {
	var proof struct {
		JobUID string `json:"job_uid"`
		Source string `json:"source"`
	}
	if json.Unmarshal(evidence, &proof) != nil || proof.JobUID == "" || proof.JobUID != want.Run.AttemptUID || (proof.Source != "job" && proof.Source != "pods") {
		return fmt.Errorf("invalid termination evidence identity")
	}
	return r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		got, err := lockRecovery(ctx, tx, tenant, want)
		if err != nil {
			return err
		}
		if jobrun.UsesPodEvidenceRetention(got.Run.TerminationEvidence) {
			// Legacy observations cannot downgrade a committed retained proof.
			return ErrRunChanged
		}
		// Cancellation may have committed after the caller took its snapshot.
		// Re-read its priority under the same run/attempt locks as the proof write.
		canceling := got.Run.Phase == jobrun.CancelRequested || got.Run.Phase == jobrun.CancelUnknown
		if canceling && (proof.Source != "pods" || !jobrun.HasPodTerminationEvidence(evidence, got.Run.AttemptUID) ||
			got.Run.UnknownSince != nil || (got.Run.Phase == jobrun.CancelUnknown && !jobrun.HasPodTerminationEvidence(got.Run.TerminationEvidence, got.Run.AttemptUID))) {
			return ErrRunChanged
		}
		var existing struct {
			Source string `json:"source"`
		}
		if !canceling && len(got.Run.TerminationEvidence) > 0 && json.Unmarshal(got.Run.TerminationEvidence, &existing) == nil && existing.Source == "job" {
			return nil
		}
		if _, err = tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET termination_evidence=$1::jsonb WHERE tenant_id=$2 AND run_id=$3 AND attempt_number=$4`, string(evidence), tenant, got.Run.ID, got.Run.Attempt); err != nil {
			return err
		}
		return insertControlPlaneAudit(ctx, tx, tenant, got.Actor, "execution.stop_observed", auditResourceRun, strconv.FormatInt(got.Run.ID, 10), map[string]any{"attempt": got.Run.Attempt, "job_uid": proof.JobUID, "source": proof.Source})
	})
}

func (r *ControlPlaneRepository) ClearExecutionUnknown(ctx context.Context, tenant string, want jobrun.Execution, ownerUID string) error {
	return r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		got, err := lockRecovery(ctx, tx, tenant, want)
		if err != nil {
			return err
		}
		if got.Run.Phase == jobrun.CancelRequested || got.Run.Phase == jobrun.CancelUnknown ||
			(got.Run.UnknownSince != nil && jobrun.UsesPodEvidenceRetention(got.Run.TerminationEvidence)) {
			return ErrRunChanged
		}
		if got.Run.OwnerRunUID != "" && got.Run.OwnerRunUID != ownerUID {
			return ErrAttemptOwnership
		}
		// Matching ownership resolves the identity gap, not the Job phase.
		// Keep the attempt Unknown until ReconcileJob records the observation;
		// replacing it with Running would reject a same-RV terminal Job fact.
		if _, err = tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET owner_run_uid=COALESCE(owner_run_uid,NULLIF($1,'')),execution_unknown_since=NULL,execution_unknown_reason=NULL WHERE tenant_id=$2 AND run_id=$3 AND attempt_number=$4`, ownerUID, tenant, got.Run.ID, got.Run.Attempt); err != nil {
			return err
		}
		if got.Run.Phase != jobrun.ExecutionUnknown {
			return nil
		}
		if _, err = tx.ExecContext(ctx, `UPDATE job_run_control_plane SET phase='Running',updated_at=$1 WHERE tenant_id=$2 AND id=$3`, r.now().UTC(), tenant, got.Run.ID); err != nil {
			return err
		}
		return insertControlPlaneAudit(ctx, tx, tenant, got.Actor, "execution.recovered", auditResourceRun, strconv.FormatInt(got.Run.ID, 10), map[string]any{"attempt": got.Run.Attempt, "job_uid": got.Run.AttemptUID})
	})
}

// InvalidatePodTerminationEvidence isolates a definite legacy evidence gap.
// Active Pods still terminating use the separate waiting path, never this call.
func (r *ControlPlaneRepository) InvalidatePodTerminationEvidence(ctx context.Context, tenant string, want jobrun.Execution) error {
	return r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		got, err := lockRecovery(ctx, tx, tenant, want)
		if err != nil {
			return err
		}
		if jobrun.UsesPodEvidenceRetention(got.Run.TerminationEvidence) {
			return ErrRunChanged
		}
		canceling := got.Run.Phase == jobrun.CancelRequested || got.Run.Phase == jobrun.CancelUnknown
		if canceling {
			if got.Run.UnknownSince != nil {
				return nil
			}
			if _, err = tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET termination_evidence=NULL,
 execution_unknown_since=COALESCE(execution_unknown_since,$1),execution_unknown_reason=$2
 WHERE tenant_id=$3 AND run_id=$4 AND attempt_number=$5`, r.now().UTC(), jobrun.ReasonPodIdentityAmbiguous, tenant, got.Run.ID, got.Run.Attempt); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE job_run_control_plane SET phase='CancelUnknown',updated_at=$1 WHERE tenant_id=$2 AND id=$3 AND phase='CancelRequested'`, r.now().UTC(), tenant, got.Run.ID); err != nil {
				return err
			}
			parent, err := lockWorkflowParentForObservation(ctx, tx, tenant, got.Run.ID)
			if err != nil {
				return err
			}
			if err = isolateWorkflowForUnknown(ctx, tx, tenant, parent, r.now().UTC()); err != nil {
				return err
			}
		} else {
			result, err := tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET termination_evidence=NULL WHERE tenant_id=$1 AND run_id=$2 AND attempt_number=$3 AND termination_evidence->>'source'='pods'`, tenant, got.Run.ID, got.Run.Attempt)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil || n == 0 {
				return err
			}
		}
		return insertControlPlaneAudit(ctx, tx, tenant, got.Actor, "execution.stop_invalidated", auditResourceRun, strconv.FormatInt(got.Run.ID, 10), map[string]any{"attempt": got.Run.Attempt, "job_uid": got.Run.AttemptUID})
	})
}

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	"orbitjob/internal/core/domain/jobrun"
)

func sameEvidence(left, right []byte, ignoreRelease bool) bool {
	var a, b map[string]any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return false
	}
	if ignoreRelease {
		delete(a, "hold_release")
		delete(b, "hold_release")
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func evidenceIdentity(data []byte, target jobrun.RecordedAttempt) bool {
	var proof struct {
		Namespace string `json:"namespace"`
		Name      string `json:"job_name"`
	}
	return json.Unmarshal(data, &proof) == nil && proof.Namespace == target.Execution.Namespace &&
		proof.Name == target.JobName && jobrun.HasRetainedTerminationEvidence(data, target.JobUID)
}

// RecordAttemptTerminationEvidence creates one immutable complete inventory.
// Release progress may change, but a later smaller sample cannot replace it.
func (r *ControlPlaneRepository) RecordAttemptTerminationEvidence(ctx context.Context, tenant string, want jobrun.RecordedAttempt, evidence []byte) error {
	if !evidenceIdentity(evidence, want) || !jobrun.EvidenceReleasePending(evidence) {
		return fmt.Errorf("retained evidence requires the original identity and a pending release")
	}
	return r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		got, _, err := lockRecordedAttempt(ctx, tx, tenant, want)
		if err != nil {
			return err
		}
		if got.UnknownSince != nil {
			return ErrRunChanged
		}
		if jobrun.UsesPodEvidenceRetention(got.TerminationEvidence) {
			if !jobrun.HasRetainedTerminationEvidence(got.TerminationEvidence, got.JobUID) || !sameEvidence(got.TerminationEvidence, evidence, true) {
				return ErrRunChanged
			}
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET termination_evidence=$1::jsonb WHERE tenant_id=$2 AND id=$3`, string(evidence), tenant, got.ID); err != nil {
			return err
		}
		return insertControlPlaneAudit(ctx, tx, tenant, got.Execution.Actor, "execution.stop_observed", auditResourceRun,
			strconv.FormatInt(got.Execution.Run.ID, 10), map[string]any{"attempt": got.Number, "job_uid": got.JobUID, "source": "pods", "protocol": jobrun.PodHoldProtocol})
	})
}

// MarkEvidenceReleased is a compare-and-set against the exact committed proof.
// A gap or another proof observed during external patch calls cannot be lost.
func (r *ControlPlaneRepository) MarkEvidenceReleased(ctx context.Context, tenant string, want jobrun.RecordedAttempt) error {
	if !evidenceIdentity(want.TerminationEvidence, want) {
		return fmt.Errorf("release requires a committed retained proof")
	}
	return r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		got, _, err := lockRecordedAttempt(ctx, tx, tenant, want)
		if err != nil {
			return err
		}
		if got.UnknownSince != nil || !evidenceIdentity(got.TerminationEvidence, got) || !sameEvidence(want.TerminationEvidence, got.TerminationEvidence, true) {
			return ErrRunChanged
		}
		if jobrun.EvidenceReleaseComplete(got.TerminationEvidence) {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET termination_evidence=jsonb_set(termination_evidence,'{hold_release}','"done"'::jsonb) WHERE tenant_id=$1 AND id=$2`, tenant, got.ID); err != nil {
			return err
		}
		return insertControlPlaneAudit(ctx, tx, tenant, got.Execution.Actor, "execution.evidence_released", auditResourceAttempt,
			strconv.FormatInt(got.ID, 10), map[string]any{"run_id": got.Execution.Run.ID, "attempt": got.Number, "job_uid": got.JobUID})
	})
}

// IsolateAttemptEvidence records the first definite gap even without an earlier
// proof. Its retained checkpoint prevents cleanup from forgetting the holds.
func (r *ControlPlaneRepository) IsolateAttemptEvidence(ctx context.Context, tenant string, want jobrun.RecordedAttempt, reason string) error {
	if reason != jobrun.ReasonJobNotFound && reason != jobrun.ReasonJobUIDMismatch && reason != jobrun.ReasonPodIdentityAmbiguous {
		return fmt.Errorf("invalid evidence isolation reason")
	}
	return r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		got, parent, err := lockRecordedAttempt(ctx, tx, tenant, want)
		if err != nil {
			return err
		}
		if got.UnknownSince != nil {
			return nil
		}
		// Keep a previously committed inventory for diagnosis, but revoke its
		// authority. A first gap must retain the same cleanup obligation.
		checkpoint := map[string]any{}
		if len(got.TerminationEvidence) > 0 {
			if err := json.Unmarshal(got.TerminationEvidence, &checkpoint); err != nil {
				return err
			}
		}
		checkpoint["protocol"], checkpoint["hold_release"], checkpoint["revoked"] = jobrun.PodHoldProtocol, "pending", true
		checkpoint["source"], checkpoint["job_uid"] = "pods", got.JobUID
		checkpoint["job_name"], checkpoint["namespace"] = got.JobName, got.Execution.Namespace
		data, err := json.Marshal(checkpoint)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET termination_evidence=$1::jsonb,
 execution_unknown_since=COALESCE(execution_unknown_since,$2),execution_unknown_reason=$3
 WHERE tenant_id=$4 AND id=$5`, string(data), r.now().UTC(), reason, tenant, got.ID); err != nil {
			return err
		}
		if got.Number == got.Execution.Run.Attempt && !jobrun.Terminal(got.Execution.Run.Phase) {
			phase := jobrun.ExecutionUnknown
			if got.Execution.Run.Phase == jobrun.CancelRequested || got.Execution.Run.Phase == jobrun.CancelUnknown {
				phase = jobrun.CancelUnknown
			}
			if _, err = tx.ExecContext(ctx, `UPDATE job_run_control_plane SET phase=$1::text,updated_at=CASE WHEN phase=$1::text THEN updated_at ELSE $2 END WHERE tenant_id=$3 AND id=$4`, phase, r.now().UTC(), tenant, got.Execution.Run.ID); err != nil {
				return err
			}
			if err = isolateWorkflowForUnknown(ctx, tx, tenant, parent, r.now().UTC()); err != nil {
				return err
			}
		}
		return insertControlPlaneAudit(ctx, tx, tenant, got.Execution.Actor, "execution.evidence_gap", auditResourceAttempt,
			strconv.FormatInt(got.ID, 10), map[string]any{"run_id": got.Execution.Run.ID, "attempt": got.Number, "job_uid": got.JobUID, "reason": reason})
	})
}

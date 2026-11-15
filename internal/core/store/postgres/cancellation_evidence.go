package postgres

import (
	"context"
	"database/sql"

	"orbitjob/internal/core/domain/jobrun"
)

// The run lock serializes confirmation with every evidence mutation. A proof
// read before this transaction is insufficient: another observer may revoke it.
func requireCancellationEvidence(ctx context.Context, tx *sql.Tx, tenant string, id int64, attempt int) error {
	var phase jobrun.Phase
	var current int
	if err := tx.QueryRowContext(ctx, `SELECT phase,attempt FROM job_run_control_plane WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&phase, &current); err != nil {
		return err
	}
	if jobrun.Terminal(phase) {
		if phase == jobrun.Canceled && current == attempt {
			return nil
		}
		return jobrun.ErrRunTerminal
	}
	if current != attempt || (phase != jobrun.CancelRequested && phase != jobrun.CancelUnknown) {
		return ErrRunChanged
	}
	var uid string
	var since sql.NullTime
	var proof []byte
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(kubernetes_job_uid,''),execution_unknown_since,termination_evidence FROM job_run_attempts_control_plane WHERE tenant_id=$1 AND run_id=$2 AND attempt_number=$3 FOR UPDATE`, tenant, id, attempt).Scan(&uid, &since, &proof); err != nil {
		return err
	}
	trusted := jobrun.HasPodTerminationEvidence(proof, uid)
	if jobrun.UsesPodEvidenceRetention(proof) {
		trusted = jobrun.HasRetainedTerminationEvidence(proof, uid) && jobrun.EvidenceReleaseComplete(proof)
	}
	if since.Valid || !trusted {
		return ErrRunChanged
	}
	return nil
}

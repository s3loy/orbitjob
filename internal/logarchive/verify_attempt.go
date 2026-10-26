package logarchive

import (
	"context"
	"fmt"
)

func (r Repository) VerifyAttempt(ctx context.Context, id Identity, source Source) error {
	tx, e := tenantTx(ctx, r.DB, id.TenantID)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	var jobUID, runUID string
	e = tx.QueryRowContext(ctx, `SELECT COALESCE(a.kubernetes_job_uid,''),COALESCE(a.owner_run_uid,'') FROM job_run_control_plane r JOIN job_run_attempts_control_plane a ON a.run_id=r.id AND a.tenant_id=r.tenant_id
 WHERE r.tenant_id=$1 AND r.id=$2 AND r.revision_id=$3 AND r.source_uid=$4 AND r.occurrence_key=$5
 AND a.attempt_number=$6`, id.TenantID, id.RunID, id.RevisionID, id.SourceUID, id.OccurrenceKey, source.Attempt).Scan(&jobUID, &runUID)
	if e != nil {
		return e
	}
	if jobUID == "" || runUID == "" {
		return fmt.Errorf("execution attempt binding not committed or identity differs")
	}
	if jobUID != source.JobUID || runUID != id.RunUID {
		return fmt.Errorf("%w: durable execution attempt ownership differs", ErrInvalidSource)
	}
	return tx.Commit()
}

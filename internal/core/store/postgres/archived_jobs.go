package postgres

import (
	"context"
	"database/sql"
	"orbitjob/internal/core/app/controlplane"
)

func (r *ControlPlaneRepository) ArchivedJobs(ctx context.Context, tenant string) ([]controlplane.ArchivedJob, error) {
	var out []controlplane.ArchivedJob
	err := r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT r.id,d.source_namespace,a.kubernetes_job_name,a.kubernetes_job_uid,
 e.identity_json->>'run_name',a.owner_run_uid,r.revision_id,a.attempt_number
 FROM execution_log_archives e JOIN job_run_control_plane r ON r.tenant_id=e.tenant_id AND r.id=e.run_id
 JOIN job_run_attempts_control_plane a ON a.tenant_id=r.tenant_id AND a.run_id=r.id
 JOIN job_definition_revisions d ON d.tenant_id=r.tenant_id AND d.id=r.revision_id
 WHERE e.tenant_id=$1 AND e.protected_until IS NOT NULL AND e.updated_at<now()-interval '5 minutes'
 AND e.completed_at IS NOT NULL AND e.state IN ('sealed','partial')
 AND r.phase IN ('Succeeded','Failed','Canceled') AND a.phase IN ('Succeeded','Failed','Canceled')
 AND a.kubernetes_job_uid IS NOT NULL AND a.owner_run_uid=e.identity_json->>'run_uid'
 AND NOT (`+pendingEvidenceSQL+`)
 AND a.log_resources_cleaned_at IS NULL
 ORDER BY e.updated_at,a.run_id,a.attempt_number LIMIT 100`, tenant)
		if e != nil {
			return e
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var job controlplane.ArchivedJob
			if e = rows.Scan(&job.RunID, &job.Namespace, &job.JobName, &job.JobUID, &job.RunName, &job.RunUID, &job.RevisionID, &job.Attempt); e != nil {
				return e
			}
			out = append(out, job)
		}
		return rows.Err()
	})
	return out, err
}

func (r *ControlPlaneRepository) MarkArchivedJobCleaned(ctx context.Context, tenant string, ref controlplane.ArchivedJob) error {
	return r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane a SET log_resources_cleaned_at=now()
 WHERE tenant_id=$1 AND run_id=$2 AND attempt_number=$3 AND kubernetes_job_uid=$4 AND owner_run_uid=$5
 AND NOT (`+pendingEvidenceSQL+`)
 AND log_resources_cleaned_at IS NULL`, tenant, ref.RunID, ref.Attempt, ref.JobUID, ref.RunUID)
		return e
	})
}

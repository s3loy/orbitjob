package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"orbitjob/internal/admin/logs"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/domain/resource"
)

type LogLocatorRepository struct{ DB *sql.DB }

func (r LogLocatorRepository) Resolve(ctx context.Context, tenant string, runID int64, attempt int) (logs.Locator, error) {
	return r.resolve(ctx, tenant, runID, 0, "", attempt)
}
func (r LogLocatorRepository) ResolveFunction(ctx context.Context, tenant string, id int64, runUUID string, attempt int) (logs.Locator, error) {
	return r.resolve(ctx, tenant, 0, id, runUUID, attempt)
}
func (r LogLocatorRepository) resolve(ctx context.Context, tenant string, runID, functionID int64, runUUID string, attempt int) (logs.Locator, error) {
	tx, err := WithTenant(ctx, r.DB, tenant)
	if err != nil {
		return logs.Locator{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var out logs.Locator
	out.TenantID = tenant
	out.Attempt = attempt
	args := []any{tenant, attempt, runID}
	where := "r.id=$3"
	group := "''"
	if functionID > 0 {
		// The projection UUID uses SHA256 for legacy non-64-character occurrence keys and
		// the first 128 bits of the occurrence digest otherwise, matching operator.
		where = `r.source_uid=$3 AND EXISTS (SELECT 1 FROM function_runs fr
   WHERE fr.tenant_id=r.tenant_id AND fr.function_id=$4 AND fr.run_id=$5::uuid
   AND replace(fr.run_id::text,'-','') = substr(CASE WHEN length(rtrim(r.occurrence_key))=64
    THEN rtrim(r.occurrence_key) ELSE encode(digest(rtrim(r.occurrence_key),'sha256'),'hex') END,1,32))`
		group = `COALESCE((SELECT resource_group_id::text FROM functions f WHERE f.id=$4 AND f.tenant_id=r.tenant_id),'')`
		args = []any{tenant, attempt, function.SourceUID(functionID), functionID, runUUID}
	}
	rows, err := tx.QueryContext(ctx, `SELECT d.source_namespace,d.source_name,r.source_uid,r.revision_id,
 rtrim(r.occurrence_key),COALESCE(a.kubernetes_job_name,''),COALESCE(a.kubernetes_job_uid,''),`+group+`
 FROM job_run_control_plane r JOIN job_definition_revisions d ON d.id=r.revision_id AND d.tenant_id=r.tenant_id
 JOIN job_run_attempts_control_plane a ON a.run_id=r.id AND a.tenant_id=r.tenant_id AND a.attempt_number=$2
 WHERE r.tenant_id=$1 AND `+where+` LIMIT 2`, args...)
	if err != nil {
		return out, fmt.Errorf("resolve log locator: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return out, err
		}
		return out, &resource.NotFoundError{Resource: "execution attempt"}
	}
	if err = rows.Scan(&out.Namespace, &out.SourceName, &out.SourceUID, &out.RevisionID, &out.OccurrenceKey, &out.JobName, &out.JobUID, &out.GroupID); err != nil {
		return out, err
	}
	if rows.Next() {
		return logs.Locator{}, &resource.ConflictError{Resource: "execution", Message: "ambiguous history-to-ledger mapping"}
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

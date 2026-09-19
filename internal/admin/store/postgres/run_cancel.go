package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	runquery "orbitjob/internal/admin/app/run/query"
	"orbitjob/internal/domain/resource"
)

// GetCancelTarget reads the identity one cancel request needs: the run's
// current phase plus the definition identity and occurrence key that name its
// JobRun Custom Resource.
//
// The run row does not carry the definition name or namespace; the revision
// the run pinned does, so the query joins job_definition_revisions on
// revision_id. Both tables are SELECT-only for orbitjob_admin: like every run
// read here, this only reads. The operator is the writer that acts on the
// patched cancel request.
//
// A run outside the caller's tenant is NotFoundError, the same answer the run
// detail query gives: under tenant scoping, "another tenant's row" and "no
// such row" must be indistinguishable.
func (r *RunRepository) GetCancelTarget(ctx context.Context, in runquery.GetInput) (runquery.CancelTarget, error) {
	tx, err := WithTenant(ctx, r.db, in.TenantID)
	if err != nil {
		return runquery.CancelTarget{}, fmt.Errorf("begin run cancel target tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `
		SELECT r.phase, rtrim(r.occurrence_key), d.source_name, d.source_namespace
		FROM job_run_control_plane r
		JOIN job_definition_revisions d ON d.id = r.revision_id
		WHERE r.tenant_id = $1 AND r.id = $2
	`, in.TenantID, in.ID)

	var out runquery.CancelTarget
	if err := row.Scan(&out.Phase, &out.OccurrenceKey, &out.ScheduledJobName, &out.Namespace); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return runquery.CancelTarget{}, &resource.NotFoundError{Resource: "run", ID: in.ID}
		}
		return runquery.CancelTarget{}, fmt.Errorf("query run cancel target: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return runquery.CancelTarget{}, fmt.Errorf("commit run cancel target tx: %w", err)
	}
	return out, nil
}

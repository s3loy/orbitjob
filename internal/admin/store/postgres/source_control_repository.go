package postgres

import (
	"context"
	"database/sql"
	"errors"

	app "orbitjob/internal/admin/app/sourcecontrol"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/function"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/sourcecontrol"
)

type SourceControlRepository struct{ db *sql.DB }

func NewSourceControlRepository(db *sql.DB) *SourceControlRepository {
	return &SourceControlRepository{db: db}
}

func sourceControlTarget(ctx context.Context, tx *sql.Tx, tenant, kind string, id int64, lock bool) (app.Target, error) {
	out := app.Target{TenantID: tenant, Kind: kind, ID: id}
	var err error
	switch kind {
	case "jobs", "workflows":
		out.Mode = sourcecontrol.Kubernetes
		if kind == "workflows" {
			out.Mode = sourcecontrol.Workflow
		}
		// Definition revisions are immutable; Admin deliberately has no UPDATE
		// grant for a FOR SHARE lock on this execution ledger relation.
		err = tx.QueryRowContext(ctx, `SELECT source_uid,COALESCE(resource_group_id::text,'') FROM job_definition_revisions WHERE tenant_id=$1 AND id=$2 AND source_mode=$3 AND is_active`, tenant, id, out.Mode).Scan(&out.SourceUID, &out.ResourceGroupID)
	case "functions", "checks":
		query := `SELECT COALESCE(resource_group_id::text,'') FROM functions WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`
		out.Mode, out.SourceUID = sourcecontrol.Function, function.SourceUID(id)
		if kind == "checks" {
			query = `SELECT COALESCE(resource_group_id::text,'') FROM checks WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`
			out.Mode, out.SourceUID = sourcecontrol.Check, check.CheckSourceUID(id)
		}
		if lock {
			query += " FOR SHARE"
		}
		err = tx.QueryRowContext(ctx, query, tenant, id).Scan(&out.ResourceGroupID)
	default:
		err = sql.ErrNoRows
	}
	if errors.Is(err, sql.ErrNoRows) {
		return app.Target{}, &resource.NotFoundError{Resource: kind, ID: id}
	}
	return out, err
}

func (r *SourceControlRepository) Resolve(ctx context.Context, tenant, kind string, id int64) (app.Target, error) {
	tx, err := WithTenant(ctx, r.db, tenant)
	if err != nil {
		return app.Target{}, err
	}
	defer func() { _ = tx.Rollback() }()
	target, err := sourceControlTarget(ctx, tx, tenant, kind, id, false)
	if err != nil {
		return app.Target{}, err
	}
	if err := tx.Commit(); err != nil {
		return app.Target{}, err
	}
	return target, nil
}

func verifyControlTarget(ctx context.Context, tx *sql.Tx, target app.Target, lock bool) error {
	current, err := sourceControlTarget(ctx, tx, target.TenantID, target.Kind, target.ID, lock)
	if err != nil {
		return err
	}
	if current.Mode != target.Mode || current.SourceUID != target.SourceUID || current.ResourceGroupID != target.ResourceGroupID {
		return &resource.NotFoundError{Resource: target.Kind, ID: target.ID}
	}
	return nil
}

func (r *SourceControlRepository) Read(ctx context.Context, target app.Target) (sourcecontrol.State, error) {
	tx, err := WithTenant(ctx, r.db, target.TenantID)
	if err != nil {
		return sourcecontrol.State{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := verifyControlTarget(ctx, tx, target, true); err != nil {
		return sourcecontrol.State{}, err
	}
	state := sourcecontrol.State{Mode: target.Mode, SourceUID: target.SourceUID}
	err = tx.QueryRowContext(ctx, `SELECT version,schedule_paused,execution_paused,tombstoned,actor,updated_at FROM execution_source_controls WHERE tenant_id=$1 AND source_mode=$2 AND source_uid=$3`, target.TenantID, target.Mode, target.SourceUID).Scan(&state.Version, &state.SchedulePaused, &state.ExecutionPaused, &state.Tombstoned, &state.Actor, &state.UpdatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return sourcecontrol.State{}, err
	}
	if err := tx.Commit(); err != nil {
		return sourcecontrol.State{}, err
	}
	return state, nil
}

func (r *SourceControlRepository) Update(ctx context.Context, target app.Target, change app.Change, actor string) (sourcecontrol.State, error) {
	return corepostgres.NewSourceControlRepository(r.db).UpdateForTenantWithVerification(ctx, target.TenantID, target.Mode, target.SourceUID, change.Version, change.SchedulePaused, change.ExecutionPaused, actor, func(ctx context.Context, tx *sql.Tx) error { return verifyControlTarget(ctx, tx, target, true) })
}

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/sourcecontrol"
)

type SourceControlRepository struct{ db *sql.DB }

func NewSourceControlRepository(db *sql.DB) *SourceControlRepository {
	return &SourceControlRepository{db: db}
}

const readSourceControlSQL = `SELECT version,schedule_paused,execution_paused,tombstoned,actor,updated_at
FROM execution_source_controls WHERE tenant_id=$1 AND source_mode=$2 AND source_uid=$3`

func readSourceControl(ctx context.Context, tx *sql.Tx, tenant, mode, uid string) (sourcecontrol.State, bool, error) {
	out := sourcecontrol.State{Mode: mode, SourceUID: uid}
	err := tx.QueryRowContext(ctx, readSourceControlSQL, tenant, mode, uid).Scan(&out.Version, &out.SchedulePaused, &out.ExecutionPaused, &out.Tombstoned, &out.Actor, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, false, nil
	}
	return out, err == nil, err
}

func validateSourceControl(mode, uid string) error {
	if !sourcecontrol.ValidMode(mode) || uid == "" || len(uid) > 128 {
		return fmt.Errorf("a supported source mode and source UID are required")
	}
	return nil
}

func (r *SourceControlRepository) GetForTenant(ctx context.Context, tenant, mode, uid string) (sourcecontrol.State, bool, error) {
	if err := validateSourceControl(mode, uid); err != nil {
		return sourcecontrol.State{}, false, err
	}
	var state sourcecontrol.State
	var found bool
	err := withAdmissionTx(ctx, r.db, tenant, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		state, found, err = readSourceControl(ctx, tx, tenant, mode, uid)
		return err
	})
	return state, found, err
}

// UpdateForTenant uses the same source lock as run admission and dispatch.
// expectedVersion zero creates explicit controls for a previously unset source.
func (r *SourceControlRepository) UpdateForTenant(ctx context.Context, tenant, mode, uid string, expectedVersion int64, schedulePaused, executionPaused bool, actor string) (sourcecontrol.State, error) {
	return r.UpdateForTenantWithVerification(ctx, tenant, mode, uid, expectedVersion, schedulePaused, executionPaused, actor, nil)
}

// UpdateForTenantWithVerification lets a configuration store verify and lock
// the authorized parent after the shared source lock, in the same transaction.
func (r *SourceControlRepository) UpdateForTenantWithVerification(ctx context.Context, tenant, mode, uid string, expectedVersion int64, schedulePaused, executionPaused bool, actor string, verify func(context.Context, *sql.Tx) error) (sourcecontrol.State, error) {
	if err := validateSourceControl(mode, uid); err != nil {
		return sourcecontrol.State{}, err
	}
	if err := validateActor(actor); err != nil {
		return sourcecontrol.State{}, err
	}
	if expectedVersion < 0 {
		return sourcecontrol.State{}, fmt.Errorf("source control version must be nonnegative")
	}
	var state sourcecontrol.State
	err := withAdmissionTx(ctx, r.db, tenant, func(ctx context.Context, tx *sql.Tx) error {
		if err := lockSource(ctx, tx, tenant, mode, uid); err != nil {
			return err
		}
		if verify != nil {
			if err := verify(ctx, tx); err != nil {
				return err
			}
		}
		prior, found, err := readSourceControl(ctx, tx, tenant, mode, uid)
		if err != nil {
			return err
		}
		if prior.Version != expectedVersion {
			return &resource.ConflictError{Resource: "source_control", ID: uid, Message: "source controls changed; refresh before saving"}
		}
		if found && prior.SchedulePaused == schedulePaused && prior.ExecutionPaused == executionPaused {
			state = prior
			return nil
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO execution_source_controls(tenant_id,source_mode,source_uid,schedule_paused,execution_paused,actor)
VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,source_mode,source_uid) DO UPDATE
SET version=execution_source_controls.version+1,schedule_paused=EXCLUDED.schedule_paused,execution_paused=EXCLUDED.execution_paused,actor=EXCLUDED.actor,updated_at=clock_timestamp()`, tenant, mode, uid, schedulePaused, executionPaused, actor); err != nil {
			return err
		}
		state, _, err = readSourceControl(ctx, tx, tenant, mode, uid)
		if err != nil {
			return err
		}
		return insertControlPlaneAudit(ctx, tx, tenant, actor, "source.controls_changed", "source_control", mode+":"+uid, map[string]any{"version": state.Version, "schedule_paused": state.SchedulePaused, "execution_paused": state.ExecutionPaused, "tombstoned": state.Tombstoned})
	})
	return state, err
}

// markSourceDeleted runs under the caller's source lock. Tombstones are distinct
// from execution pause: an accepted v1 workflow still owns its pinned steps.
func markSourceDeleted(ctx context.Context, tx *sql.Tx, tenant, mode, uid string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO execution_source_controls(tenant_id,source_mode,source_uid,tombstoned,actor)
VALUES($1,$2,$3,true,'operator') ON CONFLICT(tenant_id,source_mode,source_uid) DO UPDATE
SET version=execution_source_controls.version+1,tombstoned=true,actor='operator',updated_at=clock_timestamp()
WHERE NOT execution_source_controls.tombstoned`, tenant, mode, uid)
	return err
}

func checkSourceControls(ctx context.Context, tx *sql.Tx, tenant, mode, uid string, trigger jobrun.Trigger, newRoot bool) error {
	state, _, err := readSourceControl(ctx, tx, tenant, mode, uid)
	if err != nil {
		return err
	}
	if state.ExecutionPaused {
		return &jobrun.AdmissionBlockedError{Reason: "execution_paused"}
	}
	if newRoot && state.Tombstoned {
		return &jobrun.AdmissionBlockedError{Reason: "source_deleted"}
	}
	if newRoot && (trigger == jobrun.Schedule || trigger == jobrun.Check) && state.SchedulePaused {
		return &jobrun.AdmissionBlockedError{Reason: "schedule_paused"}
	}
	return nil
}

func checkTenantExecutionActive(ctx context.Context, tx *sql.Tx) error {
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT execution_tenant_active()`).Scan(&active); err != nil {
		return err
	}
	if !active {
		return &jobrun.AdmissionBlockedError{Reason: "tenant_inactive"}
	}
	return nil
}

type executionSourceKey struct{ mode, uid string }

// lockRunExecutionSources precedes every parent/run/attempt dispatch lock. All
// source identities are read from the ledger, then locked in a stable order.
func lockRunExecutionSources(ctx context.Context, tx *sql.Tx, tenant string, runID int64) ([]executionSourceKey, jobrun.DispatchStage, error) {
	var mode, uid string
	var parentUID sql.NullString
	var stage string
	if err := tx.QueryRowContext(ctx, `SELECT d.source_mode,r.source_uid,w.source_uid,COALESCE(a.dispatch_stage,'')
FROM job_run_control_plane r JOIN job_definition_revisions d ON d.id=r.revision_id AND d.tenant_id=r.tenant_id
LEFT JOIN workflow_run_control_plane w ON w.id=r.workflow_run_id AND w.tenant_id=r.tenant_id
LEFT JOIN job_run_attempts_control_plane a ON a.run_id=r.id AND a.attempt_number=r.attempt AND a.tenant_id=r.tenant_id
WHERE r.tenant_id=$1 AND r.id=$2`, tenant, runID).Scan(&mode, &uid, &parentUID, &stage); err != nil {
		return nil, "", err
	}
	keys := []executionSourceKey{{mode: mode, uid: uid}}
	if parentUID.Valid {
		keys = append(keys, executionSourceKey{mode: sourcecontrol.Workflow, uid: parentUID.String})
	}
	sort.Slice(keys, func(i, j int) bool {
		return sourceLockKey(tenant, keys[i].mode, keys[i].uid) < sourceLockKey(tenant, keys[j].mode, keys[j].uid)
	})
	for _, key := range keys {
		if err := lockSource(ctx, tx, tenant, key.mode, key.uid); err != nil {
			return nil, "", err
		}
	}
	return keys, jobrun.DispatchStage(stage), nil
}

func checkExecutionSources(ctx context.Context, tx *sql.Tx, tenant string, keys []executionSourceKey) error {
	if err := checkTenantExecutionActive(ctx, tx); err != nil {
		return err
	}
	for _, key := range keys {
		if err := checkSourceControls(ctx, tx, tenant, key.mode, key.uid, "", false); err != nil {
			return err
		}
	}
	return nil
}

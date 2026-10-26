package logarchive

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Repository uses Runtime credentials in the archive service and Admin
// credentials for authorized reads. This is not a new narrow DB login role.
type Repository struct{ DB *sql.DB }

func (r Repository) Status(ctx context.Context, id Identity) (string, *time.Time, error) {
	tx, e := tenantTx(ctx, r.DB, id.TenantID)
	if e != nil {
		return "", nil, e
	}
	defer func() { _ = tx.Rollback() }()
	var state string
	var until sql.NullTime
	e = tx.QueryRowContext(ctx, `SELECT state,protected_until FROM execution_log_archives WHERE tenant_id=$1 AND archive_id=$2`, id.TenantID, id.Key()).Scan(&state, &until)
	if e != nil {
		return "", nil, e
	}
	if e = tx.Commit(); e != nil {
		return "", nil, e
	}
	if until.Valid {
		return state, &until.Time, nil
	}
	return state, nil, nil
}

func tenantTx(ctx context.Context, db *sql.DB, tenant string) (*sql.Tx, error) {
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant); e != nil {
		_ = tx.Rollback()
		return nil, e
	}
	return tx, nil
}

// Discover confirms the pinned revision in the authoritative tenant ledger.
// A published CR waiting for admission can register with RunID zero.
func (r Repository) Discover(ctx context.Context, id Identity) (Identity, string, *time.Time, error) {
	tx, e := tenantTx(ctx, r.DB, id.TenantID)
	if e != nil {
		return id, "", nil, e
	}
	defer func() { _ = tx.Rollback() }()
	var phase string
	var finished sql.NullTime
	e = tx.QueryRowContext(ctx, `SELECT COALESCE(r.id,0),COALESCE(r.workflow_run_id,0),COALESCE(r.phase,''),
 CASE WHEN r.phase IN ('Succeeded','Failed','Canceled','Rejected') THEN r.updated_at END
 FROM job_definition_revisions d LEFT JOIN job_run_control_plane r
 ON r.tenant_id=d.tenant_id AND r.revision_id=d.id AND r.source_uid=d.source_uid AND r.occurrence_key=$3
 WHERE d.tenant_id=$1 AND d.id=$2 AND d.source_uid=$4 AND d.source_namespace=$5 AND d.source_name=$6`,
		id.TenantID, id.RevisionID, id.OccurrenceKey, id.SourceUID, id.Namespace, id.SourceName).Scan(&id.RunID, &id.WorkflowID, &phase, &finished)
	if e != nil {
		return id, "", nil, e
	}
	if strings.HasPrefix(id.SourceUID, "function-") {
		fid, err := strconv.ParseInt(strings.TrimPrefix(id.SourceUID, "function-"), 10, 64)
		if err != nil {
			return id, "", nil, err
		}
		if e = tx.QueryRowContext(ctx, `SELECT id,COALESCE(resource_group_id::text,'') FROM functions WHERE tenant_id=$1 AND id=$2`, id.TenantID, fid).Scan(&id.FunctionID, &id.GroupID); e != nil {
			return id, "", nil, e
		}
		digest := id.OccurrenceKey
		if decoded, err := hex.DecodeString(digest); err != nil || len(decoded) != 32 {
			sum := sha256.Sum256([]byte(digest))
			digest = hex.EncodeToString(sum[:])
		}
		id.FunctionRunID = fmt.Sprintf("%s-%s-%s-%s-%s", digest[:8], digest[8:12], digest[12:16], digest[16:20], digest[20:32])
	}
	if e = tx.Commit(); e != nil {
		return id, "", nil, e
	}
	if finished.Valid {
		return id, phase, &finished.Time, nil
	}
	return id, phase, nil, nil
}

func (r Repository) Sync(ctx context.Context, m Manifest) error {
	tx, e := tenantTx(ctx, r.DB, m.Identity.TenantID)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	data, e := json.Marshal(m.Identity)
	if e != nil {
		return e
	}
	var functionRun any
	if m.Identity.FunctionRunID != "" {
		functionRun = m.Identity.FunctionRunID
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO execution_log_archives
 (tenant_id,archive_id,run_id,function_id,function_run_id,identity_json,state,protected_until,completed_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
 ON CONFLICT(tenant_id,archive_id) DO UPDATE SET run_id=EXCLUDED.run_id,
 identity_json=EXCLUDED.identity_json,state=EXCLUDED.state,
 protected_until=GREATEST(execution_log_archives.protected_until,EXCLUDED.protected_until),
 completed_at=COALESCE(execution_log_archives.completed_at,EXCLUDED.completed_at),updated_at=now()
 WHERE execution_log_archives.identity_json->>'run_uid'=EXCLUDED.identity_json->>'run_uid'
 AND (execution_log_archives.identity_json<>EXCLUDED.identity_json OR execution_log_archives.state<>EXCLUDED.state
 OR execution_log_archives.protected_until IS DISTINCT FROM EXCLUDED.protected_until
 OR execution_log_archives.completed_at IS DISTINCT FROM EXCLUDED.completed_at)`, m.Identity.TenantID, m.Identity.Key(), m.Identity.RunID, m.Identity.FunctionID, functionRun, data, m.State, m.ProtectedUntil, m.CompletedAt)
	if e != nil {
		return e
	}
	return tx.Commit()
}

func (r Repository) Resolve(ctx context.Context, tenant string, runID, functionID int64, functionRun string) (Identity, error) {
	tx, e := tenantTx(ctx, r.DB, tenant)
	if e != nil {
		return Identity{}, e
	}
	defer func() { _ = tx.Rollback() }()
	var data []byte
	if functionID > 0 {
		e = tx.QueryRowContext(ctx, `SELECT identity_json FROM execution_log_archives WHERE tenant_id=$1 AND function_id=$2 AND function_run_id=$3::uuid`, tenant, functionID, functionRun).Scan(&data)
	} else {
		e = tx.QueryRowContext(ctx, `SELECT identity_json FROM execution_log_archives WHERE tenant_id=$1 AND run_id=$2 AND run_id>0`, tenant, runID).Scan(&data)
	}
	if e == sql.ErrNoRows {
		return Identity{}, ErrMissing
	}
	if e != nil {
		return Identity{}, e
	}
	var id Identity
	if e = json.Unmarshal(data, &id); e != nil {
		return id, e
	}
	if id.TenantID != tenant {
		return Identity{}, fmt.Errorf("archive tenant identity mismatch")
	}
	if e = tx.Commit(); e != nil {
		return id, e
	}
	return id, id.Validate()
}

func (r Repository) CleanupRequested(ctx context.Context, id Identity) (bool, error) {
	tx, e := tenantTx(ctx, r.DB, id.TenantID)
	if e != nil {
		return false, e
	}
	defer func() { _ = tx.Rollback() }()
	var requested bool
	e = tx.QueryRowContext(ctx, `SELECT cleanup_requested_at IS NOT NULL FROM execution_log_archives WHERE tenant_id=$1 AND archive_id=$2`, id.TenantID, id.Key()).Scan(&requested)
	if e == sql.ErrNoRows {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	return requested, tx.Commit()
}

// Cleanup is asynchronous and its audit event commits with the request.
func (r Repository) RequestCleanup(ctx context.Context, id Identity, actor string) error {
	tx, e := tenantTx(ctx, r.DB, id.TenantID)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	result, e := tx.ExecContext(ctx, `UPDATE execution_log_archives SET cleanup_requested_at=now() WHERE tenant_id=$1 AND archive_id=$2 AND completed_at IS NOT NULL AND cleanup_requested_at IS NULL`, id.TenantID, id.Key())
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n == 0 {
		return fmt.Errorf("log cleanup requires a terminal archive without a prior request")
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,actor_type,actor_id,event_type,resource_type,resource_id,diff) VALUES($1,'api_key',$2,'delete','execution_logs',$3,$4)`, id.TenantID, actor, id.Key(), `{"state":"requested"}`)
	if e != nil {
		return e
	}
	return tx.Commit()
}

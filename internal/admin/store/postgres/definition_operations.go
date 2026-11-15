package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"orbitjob/internal/admin/definition"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

type DefinitionOperations struct{ DB *sql.DB }

const definitionOperationColumns = `request_id,actor_id,kind,namespace,name,payload_hash,state,request_json,result_json,checked_until,expires_at,created_at,updated_at,COALESCE(admitted_uid,'')`

func scanDefinitionOperation(row interface{ Scan(...any) error }) (definition.Operation, error) {
	var op definition.Operation
	var request, result []byte
	if err := row.Scan(&op.RequestID, &op.ActorID, &op.Kind, &op.Namespace, &op.Name, &op.PayloadHash, &op.State, &request, &result, &op.CheckedUntil, &op.ExpiresAt, &op.CreatedAt, &op.UpdatedAt, &op.AdmittedUID); err != nil {
		return op, err
	}
	if err := json.Unmarshal(request, &op.Request); err != nil {
		return op, err
	}
	if len(result) > 0 && string(result) != "null" {
		if err := json.Unmarshal(result, &op.Result); err != nil {
			return op, err
		}
	}
	return op, nil
}

func definitionOperationTx(ctx context.Context, db *sql.DB, s definition.Subject) (*sql.Tx, error) {
	if db == nil {
		return nil, &resource.UnavailableError{Resource: "definition operations"}
	}
	tx, err := WithTenant(ctx, db, s.TenantID)
	if err != nil {
		return nil, &resource.UnavailableError{Resource: "definition operations", Cause: err}
	}
	return tx, nil
}

func lookupDefinitionOperation(ctx context.Context, tx *sql.Tx, s definition.Subject, id string, lock bool) (definition.Operation, error) {
	query := `SELECT ` + definitionOperationColumns + ` FROM definition_operations WHERE tenant_id=$1 AND request_id=$2 AND actor_id=$3`
	if lock {
		query += ` FOR UPDATE`
	}
	op, err := scanDefinitionOperation(tx.QueryRowContext(ctx, query, s.TenantID, id, s.ActorID))
	if errors.Is(err, sql.ErrNoRows) {
		return op, &resource.NotFoundError{Resource: "definition operation"}
	}
	if err != nil {
		return op, &resource.UnavailableError{Resource: "definition operations", Cause: err}
	}
	return op, nil
}

func (r DefinitionOperations) Register(ctx context.Context, s definition.Subject, op definition.Operation) error {
	tx, err := definitionOperationTx(ctx, r.DB, s)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	request, err := json.Marshal(op.Request)
	if err != nil {
		return err
	}
	result, err := json.Marshal(op.Result)
	if err != nil {
		return err
	}
	// Bound retained metadata without deleting unresolved operations to make room.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,7181))`, s.TenantID); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM definition_operations WHERE tenant_id=$1`, s.TenantID).Scan(&count); err != nil {
		return err
	}
	if count >= 100000 {
		return &resource.UnavailableError{Resource: "definition operations", Message: "operation retention capacity reached; reconcile pending operations before accepting new writes"}
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO definition_operations
 (tenant_id,request_id,actor_id,kind,namespace,name,payload_hash,state,request_json,result_json,checked_until,expires_at,created_at,updated_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,'checked',$8,$9,$10,$11,$12,$12) ON CONFLICT (tenant_id,request_id) DO NOTHING`, s.TenantID, op.RequestID, s.ActorID, op.Kind, op.Namespace, op.Name, op.PayloadHash, request, result, op.CheckedUntil, op.ExpiresAt, op.CreatedAt)
	if err != nil {
		return &resource.UnavailableError{Resource: "definition operations", Cause: err}
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	stored, err := lookupDefinitionOperation(ctx, tx, s, op.RequestID, true)
	if err != nil {
		return err
	}
	if stored.PayloadHash != op.PayloadHash || stored.State != "checked" {
		return &resource.ConflictError{Resource: "definition operation", Field: "request_id", Message: "this request already identifies another or unresolved operation; keep its identity for recovery"}
	}
	if n == 1 {
		err = insertAuditEvent(ctx, tx, newGrantEvent(s.TenantID, s.ActorID, "definition_checked", op.Kind, op.Namespace+"/"+op.Name, map[string]any{"request_id": op.RequestID, "manifest_digest": op.Request.ManifestDigest}))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r DefinitionOperations) Lookup(ctx context.Context, s definition.Subject, id string) (definition.Operation, error) {
	tx, err := definitionOperationTx(ctx, r.DB, s)
	if err != nil {
		return definition.Operation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	op, err := lookupDefinitionOperation(ctx, tx, s, id, false)
	if err != nil {
		return op, err
	}
	if err = tx.Commit(); err != nil {
		return op, err
	}
	return op, nil
}

func (r DefinitionOperations) Claim(ctx context.Context, s definition.Subject, id, hash string, now time.Time) (definition.Operation, bool, error) {
	tx, err := definitionOperationTx(ctx, r.DB, s)
	if err != nil {
		return definition.Operation{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	op, err := lookupDefinitionOperation(ctx, tx, s, id, true)
	var missing *resource.NotFoundError
	if errors.As(err, &missing) {
		return op, false, validation.New("request_id", "a server-side check is required before saving this exact intent")
	}
	if err != nil {
		return op, false, err
	}
	if op.PayloadHash != hash {
		return op, false, &resource.ConflictError{Resource: "definition operation", Field: "request_id", Message: "checked configuration, target or baseline changed; check and review a new intent"}
	}
	if op.State != "checked" {
		if op.State == "rejected" || op.State == "expired" {
			return op, false, &resource.ConflictError{Resource: "definition operation", Field: "request_id", Message: "this operation is closed; review its result before preparing a new intent"}
		}
		return op, false, tx.Commit()
	}
	if !now.Before(op.CheckedUntil) || !now.Before(op.ExpiresAt) {
		return op, false, &resource.ConflictError{Resource: "definition operation", Field: "checked_until", Message: "the check expired; check and review the definition again"}
	}
	var proof [32]byte
	if _, err = rand.Read(proof[:]); err != nil {
		return op, false, err
	}
	op.AdmissionProof = hex.EncodeToString(proof[:])
	proofHash := sha256.Sum256([]byte(op.AdmissionProof))
	if _, err = tx.ExecContext(ctx, `UPDATE definition_operations SET state='dispatching',updated_at=$1,admission_nonce_hash=$5 WHERE tenant_id=$2 AND request_id=$3 AND actor_id=$4`, now, s.TenantID, id, s.ActorID, hex.EncodeToString(proofHash[:])); err != nil {
		return op, false, err
	}
	if err = insertAuditEvent(ctx, tx, newGrantEvent(s.TenantID, s.ActorID, "definition_dispatched", op.Kind, op.Namespace+"/"+op.Name, map[string]any{"request_id": id, "manifest_digest": op.Request.ManifestDigest})); err != nil {
		return op, false, err
	}
	if err = tx.Commit(); err != nil {
		return op, false, err
	}
	op.State, op.UpdatedAt = "dispatching", now
	return op, true, nil
}

func (r DefinitionOperations) Finish(ctx context.Context, s definition.Subject, id, state string, result *definition.Result, now time.Time) error {
	if state != "accepted" && state != "unknown" && state != "rejected" {
		return fmt.Errorf("invalid definition outcome")
	}
	tx, err := definitionOperationTx(ctx, r.DB, s)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	op, err := lookupDefinitionOperation(ctx, tx, s, id, true)
	if err != nil {
		return err
	}
	if op.State == "accepted" {
		if state == "accepted" && result != nil && op.Result != nil && (result.UID != op.Result.UID || result.ManifestDigest != op.Result.ManifestDigest) {
			return &resource.ConflictError{Resource: "definition operation", Message: "accepted operation identity cannot change"}
		}
		return tx.Commit()
	}
	if op.State != "dispatching" && op.State != "unknown" {
		return &resource.ConflictError{Resource: "definition operation", Message: "operation was not dispatched"}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE definition_operations SET state=$1,result_json=$2,updated_at=$3 WHERE tenant_id=$4 AND request_id=$5 AND actor_id=$6`, state, encoded, now, s.TenantID, id, s.ActorID); err != nil {
		return err
	}
	if err = insertAuditEvent(ctx, tx, newGrantEvent(s.TenantID, s.ActorID, "definition_"+state, op.Kind, op.Namespace+"/"+op.Name, map[string]any{"request_id": id, "manifest_digest": op.Request.ManifestDigest})); err != nil {
		return err
	}
	return tx.Commit()
}

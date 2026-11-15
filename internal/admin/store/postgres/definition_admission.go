package postgres

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"time"

	"orbitjob/internal/admin/definition"
	"orbitjob/internal/domain/resource"
)

// AdmitDefinition binds a pre-persistence UID only with the original dispatch
// proof. The runtime role can update this column, never operation state.
func (r DefinitionOperations) AdmitDefinition(ctx context.Context, in definition.AdmissionIntent) error {
	subject := definition.Subject{TenantID: in.TenantID}
	tx, err := definitionOperationTx(ctx, r.DB, subject)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	op, err := scanDefinitionOperation(tx.QueryRowContext(ctx, `SELECT `+definitionOperationColumns+` FROM definition_operations WHERE tenant_id=$1 AND request_id=$2 FOR UPDATE`, in.TenantID, in.RequestID))
	if err != nil {
		return &resource.UnavailableError{Resource: "definition admission", Cause: err}
	}
	var hash string
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(admission_nonce_hash,'') FROM definition_operations WHERE tenant_id=$1 AND request_id=$2`, in.TenantID, in.RequestID).Scan(&hash); err != nil {
		return err
	}
	actual := sha256.Sum256([]byte(in.Proof))
	if len(in.Proof) != 64 || subtle.ConstantTimeCompare([]byte(hex.EncodeToString(actual[:])), []byte(hash)) != 1 {
		return fmt.Errorf("unverified definition dispatch proof")
	}
	if op.State != "dispatching" && op.State != "unknown" {
		return fmt.Errorf("definition operation is not awaiting admission")
	}
	if !time.Now().Before(op.CheckedUntil) {
		return fmt.Errorf("definition check expired before admission")
	}
	if op.Kind != in.Kind || op.Namespace != in.Namespace || op.Name != in.Name || op.Request.ManifestDigest != in.Digest || op.Request.ExpectedUID != in.OldUID || op.Request.ExpectedResourceVersion != in.OldResourceVersion || in.UID == "" {
		return fmt.Errorf("final definition differs from the reviewed operation")
	}
	if op.AdmittedUID != "" && op.AdmittedUID != in.UID {
		return fmt.Errorf("operation was admitted for a different object UID")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE definition_operations SET admitted_uid=$1 WHERE tenant_id=$2 AND request_id=$3`, in.UID, in.TenantID, in.RequestID); err != nil {
		return err
	}
	return tx.Commit()
}

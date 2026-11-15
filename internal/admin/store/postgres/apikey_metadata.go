package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/domain/resource"
)

// Metadata reads only a key inside its already-resolved owner tenant. It does
// not change authentication or bypass RLS to build a credential directory.
func (r *APIKeyRepository) Metadata(ctx context.Context, tenantID, id string) (apikey.Metadata, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return apikey.Metadata{}, fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var out apikey.Metadata
	err = tx.QueryRowContext(ctx, `
		SELECT name, COALESCE(resource_group_id, '') FROM api_keys
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id).Scan(&out.Name, &out.ResourceGroupID)
	if err == sql.ErrNoRows {
		return apikey.Metadata{}, &resource.NotFoundError{Resource: "api_key", ID: id}
	}
	if err != nil {
		return apikey.Metadata{}, fmt.Errorf("read api key metadata: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return apikey.Metadata{}, fmt.Errorf("commit tenant transaction: %w", err)
	}
	return out, nil
}

// Rename and its audit event commit together. Only name is writable here;
// revocation, prefixes, policy bindings and boundaries cannot be changed.
func (r *APIKeyRepository) Rename(ctx context.Context, tenantID, id, name string, ev audit.Event) error {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE api_keys SET name = $1 WHERE tenant_id = $2 AND id = $3`, name, tenantID, id)
	if err != nil {
		return fmt.Errorf("rename api key: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return &resource.NotFoundError{Resource: "api_key", ID: id}
	}
	if err := insertAuditEvent(ctx, tx, ev); err != nil {
		return err
	}
	return tx.Commit()
}

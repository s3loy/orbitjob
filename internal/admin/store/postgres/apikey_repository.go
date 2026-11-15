package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/domain/resource"
)

// APIKeyRepository provides persistence for API keys using the admin read/write schema.
type APIKeyRepository struct{ db *sql.DB }

// NewAPIKeyRepository creates an APIKeyRepository.
func NewAPIKeyRepository(db *sql.DB) *APIKeyRepository { return &APIKeyRepository{db: db} }

// Create inserts a new API key.
// Create writes a key and its grants in one transaction. The guards that decide
// what may be granted have already run by the time this is called; this method
// only has to keep the key and its bindings from ever being half-written.
// Create writes the key and its grants, and records the grant, in one
// transaction.
func (r *APIKeyRepository) Create(ctx context.Context, in apikey.PersistInput, ev audit.Event) error {
	tx, err := WithTenant(ctx, r.db, in.TenantID)
	if err != nil {
		return fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO api_keys (id, tenant_id, kind, resource_group_id, boundary_policy_id,
		                      key_hash, key_prefix, created_by, name)
		VALUES ($1, $2, 'tenant', $3, $4, $5, $6, $7, $8)
	`, in.ID, in.TenantID,
		nullIfEmpty(in.ResourceGroupID), nullIfEmpty(in.BoundaryPolicyID),
		in.KeyHash, in.KeyPrefix, nullIfEmpty(in.BoundBy), in.Name); err != nil {
		return fmt.Errorf("insert api key: %w", err)
	}

	for _, policyID := range in.PolicyIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO key_policies (key_id, policy_id, bound_by) VALUES ($1, $2, $3)
		`, in.ID, policyID, nullIfEmpty(in.BoundBy)); err != nil {
			return fmt.Errorf("bind policy %s: %w", policyID, err)
		}
	}

	if err := insertAuditEvent(ctx, tx, ev); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tenant transaction: %w", err)
	}
	return nil
}

// nullIfEmpty keeps optional columns NULL rather than storing an empty string,
// so the kind/tenant check constraint and the "no group" semantics both hold.
func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// ListByTenant returns all API keys for a tenant, excluding key_hash.
// Returning []apikey.APIKey (a core/domain type) keeps the store package below
// the app layer and avoids an import cycle between admin/app and admin/store.
func (r *APIKeyRepository) ListByTenant(ctx context.Context, tenantID string) ([]apikey.APIKey, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return nil, fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
			SELECT id, key_prefix, created_at, revoked_at, name
			FROM api_keys
			WHERE tenant_id = $1
			ORDER BY created_at DESC
		`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []apikey.APIKey
	for rows.Next() {
		var item apikey.APIKey
		if err := rows.Scan(&item.ID, &item.KeyPrefix, &item.CreatedAt, &item.RevokedAt, &item.Name); err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api keys: %w", err)
	}
	if out == nil {
		out = []apikey.APIKey{}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tenant transaction: %w", err)
	}
	return out, nil
}

// FindKeyTenant returns the tenant that owns the given API key.
// Uses the orbitjob_find_key_tenant SECURITY DEFINER function to bypass RLS
// for cross-tenant admin operations.
func (r *APIKeyRepository) FindKeyTenant(ctx context.Context, id string) (string, error) {
	var tenantID sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT tenant_id FROM orbitjob_find_key_tenant($1)`, id).Scan(&tenantID)
	if err == sql.ErrNoRows || (err == nil && (!tenantID.Valid || tenantID.String == "")) {
		return "", &resource.NotFoundError{Resource: "api_key", ID: id}
	}
	if err != nil {
		return "", fmt.Errorf("find key tenant: %w", err)
	}
	return tenantID.String, nil
}

// Scope resolves ownership without granting permission; the route guard uses
// this metadata before allowing the separately tenant-scoped audited write.
func (r *APIKeyRepository) Scope(ctx context.Context, id string) (apikey.APIKey, error) {
	tenantID, err := r.FindKeyTenant(ctx, id)
	if err != nil {
		return apikey.APIKey{}, err
	}
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return apikey.APIKey{}, err
	}
	defer func() { _ = tx.Rollback() }()
	out := apikey.APIKey{ID: id, TenantID: tenantID}
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(resource_group_id::text,'') FROM api_keys WHERE tenant_id = $1 AND id = $2 AND revoked_at IS NULL`, tenantID, id).Scan(&out.ResourceGroupID)
	if err == sql.ErrNoRows {
		return apikey.APIKey{}, &resource.NotFoundError{Resource: "api_key", ID: id}
	}
	if err != nil {
		return apikey.APIKey{}, fmt.Errorf("read key scope: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return apikey.APIKey{}, err
	}
	return out, nil
}

// Revoke marks an API key as revoked if it belongs to the tenant and is not
// already revoked, recording the revocation in the same transaction.
func (r *APIKeyRepository) Revoke(ctx context.Context, tenantID, id string, ev audit.Event) error {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, `
			UPDATE api_keys SET revoked_at = $1
			WHERE id = $2 AND tenant_id = $3 AND revoked_at IS NULL
		`, now, id, tenantID)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return &resource.NotFoundError{Resource: "api_key", ID: id}
	}

	// Recorded only once the update is known to have matched a row, so a
	// revocation that never happened leaves no trace claiming it did.
	if err := insertAuditEvent(ctx, tx, ev); err != nil {
		return err
	}
	return tx.Commit()
}

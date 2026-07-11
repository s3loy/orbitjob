package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/domain/resource"
)

// APIKeyRepository provides persistence for API keys using the admin read/write schema.
type APIKeyRepository struct{ db *sql.DB }

// NewAPIKeyRepository creates an APIKeyRepository.
func NewAPIKeyRepository(db *sql.DB) *APIKeyRepository { return &APIKeyRepository{db: db} }

// Create inserts a new API key.
func (r *APIKeyRepository) Create(ctx context.Context, tenantID, id, keyHash, keyPrefix string) error {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO api_keys (id, tenant_id, key_hash, key_prefix)
		VALUES ($1, $2, $3, $4)
	`, id, tenantID, keyHash, keyPrefix)
	if err != nil {
		return fmt.Errorf("insert api key: %w", err)
	}
	return tx.Commit()
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
		SELECT id, key_prefix, created_at, revoked_at
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
		if err := rows.Scan(&item.ID, &item.KeyPrefix, &item.CreatedAt, &item.RevokedAt); err != nil {
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
// This query bypasses RLS — only call for admin cross-tenant operations.
func (r *APIKeyRepository) FindKeyTenant(ctx context.Context, id string) (string, error) {
	var tenantID string
	err := r.db.QueryRowContext(ctx, `
		SELECT tenant_id FROM api_keys
		WHERE id = $1 AND revoked_at IS NULL
	`, id).Scan(&tenantID)
	if err == sql.ErrNoRows {
		return "", &resource.NotFoundError{Resource: "api_key", ID: id}
	}
	if err != nil {
		return "", fmt.Errorf("find key tenant: %w", err)
	}
	return tenantID, nil
}

// RevokeCrossTenant finds the key's owning tenant and revokes it in that context.
// Used by admin operations where the auth tenant differs from the key's tenant.
func (r *APIKeyRepository) RevokeCrossTenant(ctx context.Context, id string) error {
	tenantID, err := r.FindKeyTenant(ctx, id)
	if err != nil {
		return err
	}
	return r.Revoke(ctx, tenantID, id)
}

// Revoke marks an API key as revoked if it belongs to the tenant and is not already revoked.
func (r *APIKeyRepository) Revoke(ctx context.Context, tenantID, id string) error {
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
	return tx.Commit()
}

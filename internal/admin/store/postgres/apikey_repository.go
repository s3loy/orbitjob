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
func (r *APIKeyRepository) Create(ctx context.Context, tenantID, id, keyHash, keyPrefix string, createdAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO api_keys (id, tenant_id, key_hash, key_prefix, permissions, created_at)
		VALUES ($1, $2, $3, $4, '{}', $5)
	`, id, tenantID, keyHash, keyPrefix, createdAt)
	if err != nil {
		return fmt.Errorf("insert api key: %w", err)
	}
	return nil
}

// ListByTenant returns all API keys for a tenant, excluding key_hash.
func (r *APIKeyRepository) ListByTenant(ctx context.Context, tenantID string) ([]apikey.APIKey, error) {
	rows, err := r.db.QueryContext(ctx, `
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
	return out, nil
}

// Revoke marks an API key as revoked if it belongs to the tenant and is not already revoked.
func (r *APIKeyRepository) Revoke(ctx context.Context, tenantID, id string) error {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
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
	return nil
}

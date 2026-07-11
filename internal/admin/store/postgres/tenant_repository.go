package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"

	"orbitjob/internal/core/domain/tenant"
	"orbitjob/internal/domain/resource"
)

// TenantRepository provides persistence for tenants using the admin read/write schema.
type TenantRepository struct {
	db *sql.DB
}

// NewTenantRepository creates a TenantRepository.
func NewTenantRepository(db *sql.DB) *TenantRepository {
	return &TenantRepository{db: db}
}

// Create inserts a new tenant. The tenant ID is used as the RLS scope for the
// transaction so that the INSERT satisfies the tenants table WITH CHECK policy.
func (r *TenantRepository) Create(ctx context.Context, t *tenant.Tenant) error {
	tx, err := WithTenant(ctx, r.db, t.ID)
	if err != nil {
		return fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO tenants (id, slug, name, status, created_at, updated_at)
		VALUES ($1, $2, $3, COALESCE($4, 'active'), now(), now())
	`, t.ID, t.Slug, t.Name, t.Status)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return &resource.ConflictError{
				Resource: "tenant",
				Field:    "slug",
				Message:  fmt.Sprintf("tenant with slug %q already exists", t.Slug),
			}
		}
		return fmt.Errorf("insert tenant: %w", err)
	}
	return tx.Commit()
}

// List returns a paginated list of tenants ordered by creation time descending.
// RLS restricts the result to the tenant matching tenantID.
func (r *TenantRepository) List(ctx context.Context, tenantID string, limit, offset int) ([]tenant.Tenant, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return nil, fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, slug, name, status FROM tenants
		ORDER BY created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []tenant.Tenant
	for rows.Next() {
		var item tenant.Tenant
		if err := rows.Scan(&item.ID, &item.Slug, &item.Name, &item.Status); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenants: %w", err)
	}
	if out == nil {
		out = []tenant.Tenant{}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tenant transaction: %w", err)
	}
	return out, nil
}

// Get returns one tenant by ID. RLS ensures the row is only visible when it
// belongs to tenantID.
func (r *TenantRepository) Get(ctx context.Context, tenantID, id string) (tenant.Tenant, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return tenant.Tenant{}, fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var out tenant.Tenant
	err = tx.QueryRowContext(ctx, `
		SELECT id, slug, name, status FROM tenants WHERE id = $1
	`, id).Scan(&out.ID, &out.Slug, &out.Name, &out.Status)
	if err == sql.ErrNoRows {
		return tenant.Tenant{}, &resource.NotFoundError{Resource: "tenant", ID: id}
	}
	if err != nil {
		return tenant.Tenant{}, fmt.Errorf("get tenant: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return tenant.Tenant{}, fmt.Errorf("commit tenant transaction: %w", err)
	}
	return out, nil
}

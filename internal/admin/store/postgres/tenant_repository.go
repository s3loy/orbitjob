package postgres

import (
	"context"
	"database/sql"
	"fmt"

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

// Create inserts a new tenant.
func (r *TenantRepository) Create(ctx context.Context, t *tenant.Tenant) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO tenants (id, slug, name, status, created_at, updated_at)
		VALUES ($1, $2, $3, COALESCE($4, 'active'), now(), now())
	`, t.ID, t.Slug, t.Name, t.Status)
	if err != nil {
		return fmt.Errorf("insert tenant: %w", err)
	}
	return nil
}

// List returns a paginated list of tenants ordered by creation time descending.
func (r *TenantRepository) List(ctx context.Context, limit, offset int) ([]tenant.Tenant, error) {
	rows, err := r.db.QueryContext(ctx, `
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
	return out, nil
}

// Get returns one tenant by ID.
func (r *TenantRepository) Get(ctx context.Context, id string) (tenant.Tenant, error) {
	var out tenant.Tenant
	err := r.db.QueryRowContext(ctx, `
		SELECT id, slug, name, status FROM tenants WHERE id = $1
	`, id).Scan(&out.ID, &out.Slug, &out.Name, &out.Status,
	)
	if err == sql.ErrNoRows {
		return tenant.Tenant{}, &resource.NotFoundError{Resource: "tenant", ID: id}
	}
	if err != nil {
		return tenant.Tenant{}, fmt.Errorf("get tenant: %w", err)
	}
	return out, nil
}

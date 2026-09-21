package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"

	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/resourcegroup"
	"orbitjob/internal/domain/resource"
)

// ResourceGroupRepository persists resource groups, the optional isolation tier
// inside a tenant.
type ResourceGroupRepository struct{ db *sql.DB }

func NewResourceGroupRepository(db *sql.DB) *ResourceGroupRepository {
	return &ResourceGroupRepository{db: db}
}

// Create stores a group. Slug uniqueness is per tenant, so two tenants may both
// have a group called "ci" without colliding.
func (r *ResourceGroupRepository) Create(ctx context.Context, g resourcegroup.Group, ev audit.Event) error {
	tx, err := WithTenant(ctx, r.db, g.TenantID)
	if err != nil {
		return fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO resource_groups (id, tenant_id, slug, name)
		VALUES ($1, $2, $3, $4)
	`, g.ID, g.TenantID, g.Slug, g.Name)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return &resource.ConflictError{
				Resource: "resource_group",
				Field:    "slug",
				Message:  fmt.Sprintf("resource group %q already exists", g.Slug),
			}
		}
		return fmt.Errorf("insert resource group: %w", err)
	}
	if err := insertAuditEvent(ctx, tx, ev); err != nil {
		return err
	}
	return tx.Commit()
}

// List returns the tenant's groups ordered by slug, which is the order a caller
// selects from.
func (r *ResourceGroupRepository) List(ctx context.Context, tenantID string) ([]resourcegroup.Group, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return nil, fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, tenant_id, slug, name
		FROM resource_groups
		WHERE tenant_id = $1
		ORDER BY slug
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list resource groups: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []resourcegroup.Group
	for rows.Next() {
		var g resourcegroup.Group
		if err := rows.Scan(&g.ID, &g.TenantID, &g.Slug, &g.Name); err != nil {
			return nil, fmt.Errorf("scan resource group: %w", err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate resource groups: %w", err)
	}
	return out, nil
}

// Exists reports whether a group id belongs to the tenant. The key creation
// path calls it so a key cannot be scoped to a group that is not the tenant's
// own -- a foreign group id would otherwise be stored unchecked and read back
// as a scope the tenant never had.
func (r *ResourceGroupRepository) Exists(ctx context.Context, tenantID, groupID string) (bool, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return false, fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM resource_groups WHERE id = $1 AND tenant_id = $2)
	`, groupID, tenantID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check resource group %s: %w", groupID, err)
	}
	return exists, nil
}

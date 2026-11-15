package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"orbitjob/internal/domain/resource"
)

// EvidenceScopeRepository resolves the stored parent used by authorization.
// Reads remain tenant scoped under the existing Admin SELECT/RLS privileges.
type EvidenceScopeRepository struct{ db *sql.DB }

func NewEvidenceScopeRepository(db *sql.DB) *EvidenceScopeRepository {
	return &EvidenceScopeRepository{db: db}
}

func (r *EvidenceScopeRepository) CheckRunGroup(ctx context.Context, tenant string, id int64) (string, error) {
	return r.group(ctx, tenant, id, "checkrun", `SELECT COALESCE(c.resource_group_id::text,'')
 FROM check_runs r JOIN checks c ON c.tenant_id=r.tenant_id AND c.id=r.check_id
 WHERE r.tenant_id=$1 AND r.id=$2`)
}

func (r *EvidenceScopeRepository) SLOGroup(ctx context.Context, tenant string, id int64) (string, error) {
	return r.group(ctx, tenant, id, "slo", `SELECT COALESCE(resource_group_id::text,'') FROM slos WHERE tenant_id=$1 AND id=$2`)
}

func (r *EvidenceScopeRepository) AlertGroup(ctx context.Context, tenant string, id int64) (string, error) {
	return r.group(ctx, tenant, id, "sloalert", `SELECT COALESCE(s.resource_group_id::text,'')
 FROM budget_alerts a JOIN slos s ON s.tenant_id=a.tenant_id AND s.id=a.slo_id
 WHERE a.tenant_id=$1 AND a.id=$2`)
}

func (r *EvidenceScopeRepository) group(ctx context.Context, tenant string, id int64, kind, query string) (string, error) {
	tx, err := WithTenant(ctx, r.db, tenant)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	var group string
	if err := tx.QueryRowContext(ctx, query, tenant, id).Scan(&group); err != nil {
		if err == sql.ErrNoRows {
			return "", &resource.NotFoundError{Resource: kind, ID: id}
		}
		return "", fmt.Errorf("resolve %s parent scope: %w", kind, err)
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return group, nil
}

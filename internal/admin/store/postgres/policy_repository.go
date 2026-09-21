package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"

	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/resource"
)

// PolicyRepository reads policy rows and resolves what a key may actually do.
type PolicyRepository struct{ db *sql.DB }

func NewPolicyRepository(db *sql.DB) *PolicyRepository { return &PolicyRepository{db: db} }

// Get returns one policy, visible either because the tenant owns it or because
// it is a platform preset.
func (r *PolicyRepository) Get(ctx context.Context, tenantID, id string) (policy.Record, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return policy.Record{}, fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		rec     policy.Record
		ownerID sql.NullString
		raw     []byte
	)
	// The tenant predicate is enforced here, not left to row level security:
	// RLS is a second line of defence, and it is absent in the test schema and
	// bypassed entirely by a superuser connection. A policy belonging to another
	// tenant must be unreachable even then.
	err = tx.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, description, document
		FROM policies
		WHERE id = $1 AND (tenant_id = $2 OR tenant_id IS NULL)
	`, id, tenantID).Scan(&rec.ID, &ownerID, &rec.Name, &rec.Description, &raw)
	if err != nil {
		// A policy outside the tenant shares the id space with a missing one:
		// the predicate above makes a cross-tenant read empty, and both must
		// surface as NotFoundError, not as an internal error.
		if errors.Is(err, sql.ErrNoRows) {
			return policy.Record{}, &resource.NotFoundError{Resource: "policy", ID: id}
		}
		return policy.Record{}, fmt.Errorf("get policy %s: %w", id, err)
	}
	rec.TenantID = ownerID.String

	doc, err := policy.ParseDocument(raw)
	if err != nil {
		return policy.Record{}, fmt.Errorf("parse policy %s: %w", id, err)
	}
	rec.Document = policy.ResolveSelf(doc, tenantID)
	return rec, nil
}

// Create stores a tenant-owned policy.
//
// The document is stored exactly as it was validated: no normalization, no
// rewriting of resource patterns. A stored policy that differs from the one
// that was checked is a policy nobody reviewed.
func (r *PolicyRepository) Create(ctx context.Context, rec policy.Record, rawDocument []byte, ev audit.Event) error {
	tx, err := WithTenant(ctx, r.db, rec.TenantID)
	if err != nil {
		return fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO policies (id, tenant_id, name, description, document)
		VALUES ($1, $2, $3, $4, $5)
	`, rec.ID, rec.TenantID, rec.Name, rec.Description, rawDocument)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return &resource.ConflictError{
				Resource: "policy",
				Field:    "name",
				Message:  fmt.Sprintf("policy named %q already exists", rec.Name),
			}
		}
		return fmt.Errorf("insert policy: %w", err)
	}
	if err := insertAuditEvent(ctx, tx, ev); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete removes a tenant-owned policy.
//
// It refuses when the row belongs to the installation rather than the tenant.
// The distinction cannot be made by the DELETE alone -- an empty result looks
// the same whether the policy is missing or protected -- so the owner is read
// first, and the two outcomes get different answers.
func (r *PolicyRepository) Delete(ctx context.Context, tenantID, id string, ev audit.Event) error {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var owner sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT tenant_id FROM policies WHERE id = $1 AND (tenant_id = $2 OR tenant_id IS NULL)
	`, id, tenantID).Scan(&owner)
	if err == sql.ErrNoRows {
		return &resource.NotFoundError{Resource: "policy", ID: id}
	}
	if err != nil {
		return fmt.Errorf("read policy %s: %w", id, err)
	}
	if !owner.Valid {
		return policy.ErrPlatformPolicyImmutable
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM policies WHERE id = $1 AND tenant_id = $2`, id, tenantID); err != nil {
		// A policy still bound to a key is referenced by key_policies. ON DELETE
		// CASCADE would remove those bindings silently, quietly shrinking what
		// live keys can do; refusing makes the caller unbind first.
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23503" {
			return &resource.ConflictError{
				Resource: "policy",
				Field:    "id",
				Message:  "policy is still bound to one or more keys",
			}
		}
		return fmt.Errorf("delete policy %s: %w", id, err)
	}
	if err := insertAuditEvent(ctx, tx, ev); err != nil {
		return err
	}
	return tx.Commit()
}

// List returns the policies visible to a tenant: its own and the platform
// presets. A tenant sees presets because it may bind them; it cannot edit them,
// which Delete enforces.
func (r *PolicyRepository) List(ctx context.Context, tenantID string) ([]policy.Record, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return nil, fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, tenant_id, name, description, document
		FROM policies
		WHERE tenant_id = $1 OR tenant_id IS NULL
		ORDER BY tenant_id NULLS FIRST, name
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []policy.Record
	for rows.Next() {
		var (
			rec     policy.Record
			ownerID sql.NullString
			raw     []byte
		)
		if err := rows.Scan(&rec.ID, &ownerID, &rec.Name, &rec.Description, &raw); err != nil {
			return nil, fmt.Errorf("scan policy: %w", err)
		}
		rec.TenantID = ownerID.String
		doc, err := policy.ParseDocument(raw)
		if err != nil {
			return nil, fmt.Errorf("parse policy %s: %w", rec.ID, err)
		}
		// Presets carry the "self" placeholder like any other document; a tenant
		// reading the list sees them resolved against its own id, which is what
		// binding one would actually grant.
		rec.Document = policy.ResolveSelf(doc, tenantID)
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate policies: %w", err)
	}
	return out, nil
}

// EffectiveDocuments returns the documents that decide what a key may do: every
// policy bound to it, narrowed by its boundary when it has one.
//
// This is the only place the two sources are combined. Callers evaluate the
// result directly, so a key's boundary cannot be forgotten at a call site.
func (r *PolicyRepository) EffectiveDocuments(ctx context.Context, tenantID, keyID string) ([]policy.Document, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return nil, fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The key must belong to the tenant before its bindings are read, and the
	// joined policies must be the tenant's own or a platform preset.
	rows, err := tx.QueryContext(ctx, `
		SELECT p.id, p.document
		FROM key_policies kp
		JOIN policies p ON p.id = kp.policy_id
		WHERE kp.key_id = $1
		  AND EXISTS (
		    SELECT 1 FROM api_keys ak
		    WHERE ak.id = kp.key_id AND ak.tenant_id = $2
		  )
		  AND (p.tenant_id = $2 OR p.tenant_id IS NULL)
		ORDER BY p.id
	`, keyID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list bound policies for key %s: %w", keyID, err)
	}
	var documents []policy.Document
	for rows.Next() {
		var (
			id  string
			raw []byte
		)
		if err := rows.Scan(&id, &raw); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan bound policy: %w", err)
		}
		doc, err := policy.ParseDocument(raw)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("parse policy %s: %w", id, err)
		}
		documents = append(documents, policy.ResolveSelf(doc, tenantID))
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close bound policy rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bound policies: %w", err)
	}

	boundary, err := r.boundaryDocument(ctx, tx, tenantID, keyID)
	if err != nil {
		return nil, err
	}
	if boundary == nil {
		return documents, nil
	}
	return policy.Intersect(documents, *boundary), nil
}

// boundaryDocument loads the key's permission boundary, if it has one.
func (r *PolicyRepository) boundaryDocument(ctx context.Context, tx *sql.Tx, tenantID, keyID string) (*policy.Document, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `
		SELECT p.document
		FROM api_keys ak
		JOIN policies p ON p.id = ak.boundary_policy_id
		WHERE ak.id = $1
		  AND ak.tenant_id = $2
		  AND (p.tenant_id = $2 OR p.tenant_id IS NULL)
	`, keyID, tenantID).Scan(&raw)
	if err == sql.ErrNoRows {
		// No boundary set: the key is capped only by its own policies.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load boundary for key %s: %w", keyID, err)
	}
	doc, err := policy.ParseDocument(raw)
	if err != nil {
		return nil, fmt.Errorf("parse boundary for key %s: %w", keyID, err)
	}
	resolved := policy.ResolveSelf(doc, tenantID)
	return &resolved, nil
}

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"orbitjob/internal/core/domain/audit"
)

// insertAuditEvent appends an authorization-change row using the caller's
// transaction, so the record and the change it describes commit together.
//
// It takes a *sql.Tx rather than a *sql.DB on purpose. A version that opened its
// own transaction would let a grant commit and its record fail, leaving a live
// credential nobody can account for -- and, worse, would let the API report
// failure for an operation that had already taken effect. Taking the
// transaction makes that impossible to write by accident.
//
// This mirrors how the core store records job and instance transitions: the
// audit row is written inside the same transaction as the state change.
func insertAuditEvent(ctx context.Context, tx *sql.Tx, e audit.Event) error {
	if e.TenantID == "" {
		return fmt.Errorf("audit event requires a tenant")
	}
	diff, err := json.Marshal(e.Diff)
	if err != nil {
		return fmt.Errorf("encode audit diff: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_events
			(tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
		VALUES ($1, 'api_key', $2, $3, $4, $5, $6)
	`, e.TenantID, nullIfEmpty(e.ActorID), e.EventType, e.ResourceType, e.ResourceID, diff,
	); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

// newGrantEvent builds the audit row for a grant or revocation.
//
// The diff records the grant as applied rather than as requested: a key that
// inherited its creator's boundary has to read back as bounded, or the trail
// would suggest the new credential was unconstrained.
func newGrantEvent(tenantID, actorID, eventType, resourceType, resourceID string, diff map[string]any) audit.Event {
	if diff == nil {
		diff = map[string]any{}
	}
	return audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		EventType:    eventType,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Diff:         diff,
	}
}

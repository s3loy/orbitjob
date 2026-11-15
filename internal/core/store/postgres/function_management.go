package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"orbitjob/internal/core/domain/audit"
	"strconv"
	"strings"

	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/domain/resource"
)

// CreateManaged persists a definition and its audit row atomically. The
// request identity survives a lost response without granting read access.
func (r *FunctionRepository) CreateManaged(ctx context.Context, in function.ManagedWrite) (function.Definition, bool, error) {
	tx, err := r.managementTx(ctx, in.Definition.TenantID)
	if err != nil {
		return function.Definition{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	d := in.Definition
	command, args, labels, err := managedFunctionJSON(d)
	if err != nil {
		return function.Definition{}, false, err
	}
	row := tx.QueryRowContext(ctx, `INSERT INTO functions
		(name, description, tenant_id, resource_group_id, status, image, command, args,
		timeout_seconds, retry_limit, history_success, history_failed, labels,
		create_actor_id, create_idempotency_key, create_payload_hash, execution_policy_version, retry_safe, resources)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10,$11,$12,$13::jsonb,$14,$15,$16,$17,$18,$19::jsonb)
		ON CONFLICT (tenant_id, create_actor_id, create_idempotency_key)
		WHERE create_idempotency_key IS NOT NULL DO NOTHING
		RETURNING `+functionColumns,
		d.Name, nullableText(d.Description), d.TenantID, nullableGroup(d.ResourceGroupID), d.Status,
		d.Image, command, args, d.TimeoutSeconds, d.RetryLimit, d.HistorySuccess, d.HistoryFailed,
		labels, in.ActorID, in.IdempotencyKey, in.PayloadHash, d.ExecutionPolicyVersion, d.RetrySafe, nullableResourceJSON(d.Resources))
	created := true
	out, err := scanManagedFunction(row)
	if errors.Is(err, sql.ErrNoRows) {
		created = false
		var hash string
		out, hash, err = replayManagedFunction(ctx, tx, in)
		if err == nil && (hash != in.PayloadHash || out.ResourceGroupID != d.ResourceGroupID) {
			return function.Definition{}, false, &resource.ConflictError{Resource: "function", Field: "idempotency_key", Message: "the request identity was already used for another definition"}
		}
	}
	if err != nil {
		return function.Definition{}, false, fmt.Errorf("create function: %w", err)
	}
	if created {
		if err := auditManagedFunction(ctx, tx, in, out, "create", 0); err != nil {
			return function.Definition{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return function.Definition{}, false, fmt.Errorf("commit function create: %w", err)
	}
	return out, created, nil
}

// UpdateManaged locks the exact tenant/group row before testing its version.
// The immutable group cannot be moved by a payload or an optimistic retry.
func (r *FunctionRepository) UpdateManaged(ctx context.Context, in function.ManagedWrite) (function.Definition, error) {
	d := in.Definition
	tx, err := r.managementTx(ctx, d.TenantID)
	if err != nil {
		return function.Definition{}, err
	}
	defer func() { _ = tx.Rollback() }()
	previous, err := scanManagedFunction(tx.QueryRowContext(ctx, `SELECT `+functionColumns+`
		FROM functions WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL
		AND ($3::text IS NULL OR resource_group_id=$3) FOR UPDATE`, d.TenantID, d.ID, nullableGroup(in.CallerGroupID)))
	if errors.Is(err, sql.ErrNoRows) {
		return function.Definition{}, &resource.NotFoundError{Resource: "function", ID: d.ID}
	}
	if err != nil {
		return function.Definition{}, fmt.Errorf("lock function: %w", err)
	}
	if previous.Version != d.Version {
		return function.Definition{}, &resource.ConflictError{Resource: "function", ID: d.ID, Field: "expected_version", Message: "the function changed; reload it before saving"}
	}
	command, args, labels, err := managedFunctionJSON(d)
	if err != nil {
		return function.Definition{}, err
	}
	out, err := scanManagedFunction(tx.QueryRowContext(ctx, `UPDATE functions SET
		name=$3, description=$4, status=$5, image=$6, command=$7::jsonb, args=$8::jsonb,
		timeout_seconds=$9, retry_limit=$10, history_success=$11, history_failed=$12,
		labels=$13::jsonb, version=version+1, updated_at=now(),
        execution_policy_version=$15,retry_safe=$16,resources=$17::jsonb
		WHERE tenant_id=$1 AND id=$2 AND version=$14 AND deleted_at IS NULL
		RETURNING `+functionColumns, d.TenantID, d.ID, d.Name, nullableText(d.Description), d.Status,
		d.Image, command, args, d.TimeoutSeconds, d.RetryLimit, d.HistorySuccess, d.HistoryFailed, labels, d.Version, d.ExecutionPolicyVersion, d.RetrySafe, nullableResourceJSON(d.Resources)))
	if err != nil {
		return function.Definition{}, fmt.Errorf("update function: %w", err)
	}
	if err := auditManagedFunction(ctx, tx, in, out, "update", previous.Version); err != nil {
		return function.Definition{}, err
	}
	if err := tx.Commit(); err != nil {
		return function.Definition{}, fmt.Errorf("commit function update: %w", err)
	}
	return out, nil
}

func (r *FunctionRepository) managementTx(ctx context.Context, tenantID string) (*sql.Tx, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("function database is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin function management: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("set function tenant: %w", err)
	}
	return tx, nil
}

func managedFunctionJSON(d function.Definition) ([]byte, []byte, []byte, error) {
	command, err := functionJSON(d.Command)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encode command: %w", err)
	}
	args, err := functionJSON(d.Args)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encode args: %w", err)
	}
	labels, err := functionJSON(d.Labels)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encode labels: %w", err)
	}
	return command, args, labels, nil
}

func scanManagedFunction(row *sql.Row) (function.Definition, error) {
	var d function.Definition
	var command, args, labels, resources []byte
	var description, group sql.NullString
	err := row.Scan(&d.ID, &d.Name, &description, &d.TenantID, &group, &d.Status, &d.Image,
		&command, &args, &d.TimeoutSeconds, &d.RetryLimit, &d.HistorySuccess, &d.HistoryFailed,
		&labels, &d.Version, &d.CreatedAt, &d.UpdatedAt, &d.ExecutionPolicyVersion, &d.RetrySafe, &resources)
	if err == nil {
		err = unmarshalFunctionScan(&d, description, group, command, args, labels, resources)
	}
	return d, err
}

func replayManagedFunction(ctx context.Context, tx *sql.Tx, in function.ManagedWrite) (function.Definition, string, error) {
	// The matching hash authenticates the original normalized snapshot. A
	// Create-only key must not gain access to another writer's later edits.
	d := in.Definition
	d.Version = 1
	var group sql.NullString
	var hash string
	err := tx.QueryRowContext(ctx, `SELECT id, create_payload_hash, resource_group_id, created_at FROM functions
		WHERE tenant_id=$1 AND create_actor_id=$2 AND create_idempotency_key=$3
		AND deleted_at IS NULL FOR UPDATE`, in.Definition.TenantID, in.ActorID, in.IdempotencyKey).Scan(&d.ID, &hash, &group, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return function.Definition{}, "", &resource.ConflictError{Resource: "function", Field: "idempotency_key", Message: "the original function is no longer available"}
	}
	if err != nil {
		return function.Definition{}, "", err
	}
	d.ResourceGroupID, d.UpdatedAt = group.String, d.CreatedAt
	return d, hash, nil
}

func auditManagedFunction(ctx context.Context, tx *sql.Tx, in function.ManagedWrite, d function.Definition, action string, previousVersion int) error {
	diff, err := json.Marshal(map[string]any{"version": d.Version, "previous_version": previousVersion,
		"name": d.Name, "description": d.Description, "status": d.Status, "image": d.Image,
		"command": d.Command, "args": d.Args, "timeout_seconds": d.TimeoutSeconds,
		"retry_limit": d.RetryLimit, "history_success": d.HistorySuccess,
		"history_failed": d.HistoryFailed, "labels": d.Labels, "resource_group_id": d.ResourceGroupID,
		"execution_policy_version": d.ExecutionPolicyVersion, "retry_safe": d.RetrySafe, "resources": d.Resources})
	if err != nil {
		return fmt.Errorf("encode function audit: %w", err)
	}
	actorType := audit.ActorType(ctx, in.ActorID)
	query := `INSERT INTO audit_events
		(tenant_id,actor_type,actor_id,event_type,resource_type,resource_id,diff,idempotency_key)
		VALUES ($1,'api_key',$2,$3,'function',$4,$5::jsonb,NULLIF($6,''))`
	if actorType == "user" {
		query = strings.Replace(query, "'api_key'", "'user'", 1)
	}
	_, err = tx.ExecContext(ctx, query,
		d.TenantID, in.ActorID, action, strconv.FormatInt(d.ID, 10), string(diff), in.IdempotencyKey)
	if err != nil {
		return fmt.Errorf("insert function audit: %w", err)
	}
	return nil
}

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/domain/executionpolicy"
	"orbitjob/internal/domain/queryfilter"
	"orbitjob/internal/domain/resource"
)

// FunctionRepository implements FunctionStore against the functions table:
// configuration family, tenant_id CASCADE, admin DML, runtime SELECT-only.
// Every method runs inside the tenant GUC so the RLS policy is the second
// gate; the explicit tenant predicate stays because defense in depth is cheap.
type FunctionRepository struct {
	db *sql.DB
}

var _ FunctionStore = (*FunctionRepository)(nil)

func NewFunctionRepository(db *sql.DB) *FunctionRepository {
	return &FunctionRepository{db: db}
}

// functionColumns is the projection every definition read uses, in scan order.
const functionColumns = `id, name, description, tenant_id, resource_group_id, status,
	image, command, args, timeout_seconds, retry_limit, history_success, history_failed,
	labels, version, created_at, updated_at, execution_policy_version, retry_safe, resources`

// functionJSON normalizes the jsonb columns' Go shapes before marshal: a nil
// slice or map would encode as SQL null and violate the array/object CHECKs
// the table enforces on command, args and labels.
func functionJSON(v any) ([]byte, error) {
	switch typed := v.(type) {
	case []string:
		if typed == nil {
			typed = []string{}
		}
		return json.Marshal(typed)
	case map[string]any:
		if typed == nil {
			typed = map[string]any{}
		}
		return json.Marshal(typed)
	}
	return json.Marshal(v)
}

func (r *FunctionRepository) CreateForTenant(ctx context.Context, tenantID string, def function.Definition) (function.Definition, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return function.Definition{}, fmt.Errorf("begin function create tx: %w", err)
	}
	// Unconditional release: no return path below is required to keep err
	// non-nil, so a rollback keyed on it would strand the tx.
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return function.Definition{}, fmt.Errorf("set tenant context: %w", err)
	}

	commandBytes, err := functionJSON(def.Command)
	if err != nil {
		return function.Definition{}, fmt.Errorf("marshal command: %w", err)
	}
	argsBytes, err := functionJSON(def.Args)
	if err != nil {
		return function.Definition{}, fmt.Errorf("marshal args: %w", err)
	}
	labelsBytes, err := functionJSON(def.Labels)
	if err != nil {
		return function.Definition{}, fmt.Errorf("marshal labels: %w", err)
	}

	status := def.Status
	if status == "" {
		// The column's CHECK admits only active|paused; an unset status means
		// the column default, not a definition the schema would refuse.
		status = function.StatusActive
	}
	version := def.Version
	if version < 1 {
		version = 1
	}

	var snap function.Definition
	var commandRaw, argsRaw, labelsRaw, resourcesRaw []byte
	var description sql.NullString
	var resourceGroup sql.NullString
	err = tx.QueryRowContext(ctx, `
		INSERT INTO functions (
			name, description, tenant_id, resource_group_id, status, image, command, args,
			timeout_seconds, retry_limit, history_success, history_failed, labels, version, execution_policy_version, retry_safe, resources
		) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $10, $11, $12, $13::jsonb, $14, $15, $16, $17::jsonb)
		RETURNING `+functionColumns,
		def.Name, nullableText(def.Description), tenantID, nullableGroup(def.ResourceGroupID),
		status, def.Image, commandBytes, argsBytes,
		def.TimeoutSeconds, def.RetryLimit, def.HistorySuccess, def.HistoryFailed,
		labelsBytes, version, def.ExecutionPolicyVersion, def.RetrySafe, nullableResourceJSON(def.Resources),
	).Scan(
		&snap.ID, &snap.Name, &description, &snap.TenantID, &resourceGroup, &snap.Status,
		&snap.Image, &commandRaw, &argsRaw, &snap.TimeoutSeconds, &snap.RetryLimit,
		&snap.HistorySuccess, &snap.HistoryFailed, &labelsRaw, &snap.Version,
		&snap.CreatedAt, &snap.UpdatedAt, &snap.ExecutionPolicyVersion, &snap.RetrySafe, &resourcesRaw,
	)
	if err != nil {
		return function.Definition{}, fmt.Errorf("insert function: %w", err)
	}
	if err := unmarshalFunctionScan(&snap, description, resourceGroup, commandRaw, argsRaw, labelsRaw, resourcesRaw); err != nil {
		return function.Definition{}, err
	}

	if err = tx.Commit(); err != nil {
		return function.Definition{}, fmt.Errorf("commit function create: %w", err)
	}
	return snap, nil
}

func (r *FunctionRepository) UpdateForTenant(ctx context.Context, tenantID string, def function.Definition) (function.Definition, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return function.Definition{}, fmt.Errorf("begin function update tx: %w", err)
	}
	// Unconditional release: the stale-version branch below reuses err for its
	// existence probe, so a rollback keyed on it would strand the tx.
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return function.Definition{}, fmt.Errorf("set tenant context: %w", err)
	}

	commandBytes, err := functionJSON(def.Command)
	if err != nil {
		return function.Definition{}, fmt.Errorf("marshal command: %w", err)
	}
	argsBytes, err := functionJSON(def.Args)
	if err != nil {
		return function.Definition{}, fmt.Errorf("marshal args: %w", err)
	}
	labelsBytes, err := functionJSON(def.Labels)
	if err != nil {
		return function.Definition{}, fmt.Errorf("marshal labels: %w", err)
	}

	var snap function.Definition
	var commandRaw, argsRaw, labelsRaw, resourcesRaw []byte
	var description sql.NullString
	var resourceGroup sql.NullString
	err = tx.QueryRowContext(ctx, `
		UPDATE functions SET
			name = $3, description = $4, status = $5, image = $6, command = $7::jsonb, args = $8::jsonb,
			timeout_seconds = $9, retry_limit = $10, history_success = $11, history_failed = $12,
			labels = $13::jsonb, version = version + 1, updated_at = now(),
            execution_policy_version=$15, retry_safe=$16, resources=$17::jsonb
		WHERE tenant_id = $1 AND id = $2 AND version = $14 AND deleted_at IS NULL
		RETURNING `+functionColumns,
		tenantID, def.ID, def.Name, nullableText(def.Description), def.Status, def.Image,
		commandBytes, argsBytes, def.TimeoutSeconds, def.RetryLimit,
		def.HistorySuccess, def.HistoryFailed, labelsBytes, def.Version, def.ExecutionPolicyVersion, def.RetrySafe, nullableResourceJSON(def.Resources),
	).Scan(
		&snap.ID, &snap.Name, &description, &snap.TenantID, &resourceGroup, &snap.Status,
		&snap.Image, &commandRaw, &argsRaw, &snap.TimeoutSeconds, &snap.RetryLimit,
		&snap.HistorySuccess, &snap.HistoryFailed, &labelsRaw, &snap.Version,
		&snap.CreatedAt, &snap.UpdatedAt, &snap.ExecutionPolicyVersion, &snap.RetrySafe, &resourcesRaw,
	)
	if err == sql.ErrNoRows {
		// A zero-row update is either a stale Version or a definition that is
		// gone; the caller deserves to know which.
		var existingID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM functions
			WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
		`, tenantID, def.ID).Scan(&existingID); err == sql.ErrNoRows {
			return function.Definition{}, &resource.NotFoundError{Resource: "function", ID: def.ID}
		}
		return function.Definition{}, &resource.ConflictError{
			Resource: "function", ID: def.ID, Field: "version", Message: "stale function version",
		}
	}
	if err != nil {
		return function.Definition{}, fmt.Errorf("update function: %w", err)
	}
	if err := unmarshalFunctionScan(&snap, description, resourceGroup, commandRaw, argsRaw, labelsRaw, resourcesRaw); err != nil {
		return function.Definition{}, err
	}

	if err = tx.Commit(); err != nil {
		return function.Definition{}, fmt.Errorf("commit function update: %w", err)
	}
	return snap, nil
}

func (r *FunctionRepository) GetForTenant(ctx context.Context, tenantID string, id int64) (function.Definition, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return function.Definition{}, false, fmt.Errorf("begin function get tx: %w", err)
	}
	// Unconditional release: the scan below captures its error into scanErr,
	// so the not-found and scan-failure returns leave err nil and a rollback
	// keyed on err would leak the transaction holding the tenant GUC.
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return function.Definition{}, false, fmt.Errorf("set tenant context: %w", err)
	}

	var snap function.Definition
	var commandRaw, argsRaw, labelsRaw, resourcesRaw []byte
	var description sql.NullString
	var resourceGroup sql.NullString
	scanErr := tx.QueryRowContext(ctx, `
		SELECT `+functionColumns+`
		FROM functions
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, id).Scan(
		&snap.ID, &snap.Name, &description, &snap.TenantID, &resourceGroup, &snap.Status,
		&snap.Image, &commandRaw, &argsRaw, &snap.TimeoutSeconds, &snap.RetryLimit,
		&snap.HistorySuccess, &snap.HistoryFailed, &labelsRaw, &snap.Version,
		&snap.CreatedAt, &snap.UpdatedAt, &snap.ExecutionPolicyVersion, &snap.RetrySafe, &resourcesRaw,
	)
	if scanErr == sql.ErrNoRows {
		// Deleted and other-tenant rows are alike not found: RLS hides the
		// latter and the soft-delete predicate hides the former.
		return function.Definition{}, false, nil
	}
	if scanErr != nil {
		return function.Definition{}, false, fmt.Errorf("get function: %w", scanErr)
	}
	if err := unmarshalFunctionScan(&snap, description, resourceGroup, commandRaw, argsRaw, labelsRaw, resourcesRaw); err != nil {
		return function.Definition{}, false, err
	}

	if err = tx.Commit(); err != nil {
		return function.Definition{}, false, fmt.Errorf("commit function get: %w", err)
	}
	return snap, true, nil
}

func (r *FunctionRepository) ListForTenant(ctx context.Context, tenantID string) ([]function.Definition, error) {
	return r.listScoped(ctx, tenantID, false)
}

func (r *FunctionRepository) ActiveForTenant(ctx context.Context, tenantID string) ([]function.Definition, error) {
	return r.listScoped(ctx, tenantID, true)
}

// listScoped lists the tenant's live definitions, optionally narrowed to the
// active ones the operator's revision-sync loop reads. The loop materializes
// one revision per definition version idempotently, so the steady-state read
// is this list once per tick.
func (r *FunctionRepository) listScoped(ctx context.Context, tenantID string, activeOnly bool) ([]function.Definition, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin function list tx: %w", err)
	}
	// Unconditional release: the scan and rows.Err checks below capture their
	// errors into shadowed locals, so a rollback keyed on err leaked the tx
	// mid-iteration.
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return nil, fmt.Errorf("set tenant context: %w", err)
	}

	query := `SELECT ` + functionColumns + `
		FROM functions
		WHERE tenant_id = $1 AND deleted_at IS NULL`
	if activeOnly {
		query += ` AND status = '` + function.StatusActive + `'`
		query += ` ORDER BY id ASC`
	} else {
		query += ` ORDER BY created_at DESC, id DESC`
	}

	rows, err := tx.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list functions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var defs []function.Definition
	for rows.Next() {
		var snap function.Definition
		var commandRaw, argsRaw, labelsRaw, resourcesRaw []byte
		var description sql.NullString
		var resourceGroup sql.NullString
		err := rows.Scan(
			&snap.ID, &snap.Name, &description, &snap.TenantID, &resourceGroup, &snap.Status,
			&snap.Image, &commandRaw, &argsRaw, &snap.TimeoutSeconds, &snap.RetryLimit,
			&snap.HistorySuccess, &snap.HistoryFailed, &labelsRaw, &snap.Version,
			&snap.CreatedAt, &snap.UpdatedAt, &snap.ExecutionPolicyVersion, &snap.RetrySafe, &resourcesRaw,
		)
		if err != nil {
			return nil, fmt.Errorf("scan function: %w", err)
		}
		if err := unmarshalFunctionScan(&snap, description, resourceGroup, commandRaw, argsRaw, labelsRaw, resourcesRaw); err != nil {
			return nil, err
		}
		defs = append(defs, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate functions: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit function list: %w", err)
	}
	return defs, nil
}

func (r *FunctionRepository) DeleteForTenant(ctx context.Context, tenantID string, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin function delete tx: %w", err)
	}
	// Unconditional release: no return path below is required to keep err
	// non-nil, so a rollback keyed on it would strand the tx.
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	var deletedID int64
	err = tx.QueryRowContext(ctx, `
		UPDATE functions
		SET deleted_at = now(), version = version + 1, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING id
	`, tenantID, id).Scan(&deletedID)
	if err == sql.ErrNoRows {
		return &resource.NotFoundError{Resource: "function", ID: id}
	}
	if err != nil {
		return fmt.Errorf("delete function: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit function delete: %w", err)
	}
	return nil
}

// nullableText writes an empty description as SQL NULL instead of an empty
// string.
func nullableText(s string) any {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// unmarshalFunctionScan decodes the jsonb and nullable columns of one
// functions-row scan into the domain struct. Decode failures of jsonb the
// database itself validated (array/object CHECKs) are not survivable
// misencodings, but losing the row to one would hide a live definition, so
// legacy arrays retain their existing decoding behavior. New execution resources
// fail closed on malformed typed content instead of silently selecting defaults.
func unmarshalFunctionScan(snap *function.Definition, description, resourceGroup sql.NullString, commandRaw, argsRaw, labelsRaw, resourcesRaw []byte) error {
	snap.Description = description.String
	snap.ResourceGroupID = resourceGroup.String
	if len(commandRaw) > 0 {
		_ = json.Unmarshal(commandRaw, &snap.Command)
	}
	if len(argsRaw) > 0 {
		_ = json.Unmarshal(argsRaw, &snap.Args)
	}
	if len(labelsRaw) > 0 {
		_ = json.Unmarshal(labelsRaw, &snap.Labels)
	}
	if len(resourcesRaw) > 0 {
		if err := json.Unmarshal(resourcesRaw, &snap.Resources); err != nil {
			return fmt.Errorf("decode function resources: %w", err)
		}
	}
	return nil
}

// FunctionRunRepository implements FunctionRunStore against the function_runs
// read model: one row per terminal function invocation, written by the
// operator's terminal-phase bookkeeping and read by the admin API. Like
// check_runs it is not a work queue — live progress lives on the JobRun CR
// and the ledger row.
type FunctionRunRepository struct {
	db *sql.DB
}

var _ FunctionRunStore = (*FunctionRunRepository)(nil)

func NewFunctionRunRepository(db *sql.DB) *FunctionRunRepository {
	return &FunctionRunRepository{db: db}
}

// defaultRunPage is the page RunsByFunction serves when the caller does not
// name one, matching the admin list surfaces' default.
const defaultRunPage = 50

// The projection UUID identifies a terminal row, not its ledger occurrence.
// Resolve the real key under the same tenant and Function parent. A retained
// orphan or ambiguous digest prefix returns NULL instead of inventing a link.
const functionRunReadColumns = `fr.id, fr.run_id, fr.tenant_id, fr.function_id, fr.status,
	fr.triggered_at, fr.started_at, fr.finished_at, fr.duration_ms, fr.version, fr.created_at,
	(SELECT CASE WHEN count(*)=1 THEN min(rtrim(r.occurrence_key)) END
	 FROM job_run_control_plane r JOIN job_definition_revisions d
	 ON d.id=r.revision_id AND d.tenant_id=r.tenant_id
	 WHERE r.tenant_id=fr.tenant_id AND r.source_uid='function-' || fr.function_id::text
	 AND d.source_mode='function' AND d.source_uid=r.source_uid
	 AND replace(fr.run_id::text,'-','')=substr(CASE WHEN length(rtrim(r.occurrence_key))=64
	 THEN rtrim(r.occurrence_key) ELSE encode(digest(rtrim(r.occurrence_key),'sha256'),'hex') END,1,32))`

const recordCompletedFunctionRunSQL = `
INSERT INTO function_runs
  (run_id, tenant_id, function_id, status, triggered_at, started_at, finished_at, duration_ms)
VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (run_id) DO NOTHING
RETURNING id`

// RecordCompleted upserts one terminal function invocation into the read
// model. runID is the deterministic UUID derived from the ledger occurrence
// key, so a replayed recording lands on the same row instead of doubling
// history; the ON CONFLICT guard keeps the call safe even where the caller's
// exactly-once guard already holds.
//
// The status/timestamps CHECK mirrors check_runs': a success or failure both
// started and finished, a cancellation only finished. The defaults below keep
// a partially-timed record insertable rather than aborting the operator's
// bookkeeping on a null it can still answer honestly.
func (r *FunctionRunRepository) RecordCompleted(ctx context.Context, tenantID string, record function.CompletedRecord) error {
	_, err := r.RecordCompletedOnce(ctx, tenantID, record)
	return err
}

// RecordCompletedOnce reports whether this call inserted the terminal fact.
func (r *FunctionRunRepository) RecordCompletedOnce(ctx context.Context, tenantID string, record function.CompletedRecord) (created bool, err error) {
	if record.RunID == "" {
		return false, fmt.Errorf("run id is required")
	}
	if record.FunctionID < 1 {
		return false, fmt.Errorf("function id is required")
	}
	if record.Status != function.StatusSuccess && record.Status != function.StatusFailed && record.Status != function.StatusCanceled {
		return false, fmt.Errorf("status %q is not a terminal function-run status", record.Status)
	}
	if record.TriggeredAt.IsZero() {
		return false, fmt.Errorf("triggered_at is required")
	}
	if record.FinishedAt.IsZero() {
		record.FinishedAt = time.Now().UTC()
	}
	if record.Status != function.StatusCanceled && record.StartedAt.IsZero() {
		// The CHECK requires both instants on an exit outcome; the trigger
		// instant is the honest floor for a start the ledger never saw.
		record.StartedAt = record.TriggeredAt
	}
	if record.DurationMs < 0 {
		record.DurationMs = 0
	}
	// A cancellation before its first attempt has no measured execution time.
	// Keep a recorded zero duration when an attempt did start, but do not turn
	// an absent start into year 0001 or an unknown duration into zero.
	startedAt := sql.NullTime{Time: record.StartedAt, Valid: !record.StartedAt.IsZero()}
	durationMs := sql.NullInt64{Int64: int64(record.DurationMs), Valid: startedAt.Valid}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin function run record tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return false, fmt.Errorf("set tenant context: %w", err)
	}

	var id int64
	err = tx.QueryRowContext(ctx, recordCompletedFunctionRunSQL,
		record.RunID, tenantID, record.FunctionID, record.Status,
		record.TriggeredAt, startedAt, record.FinishedAt, durationMs,
	).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("record completed function run: %w", err)
	}

	created = err == nil
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit function run record: %w", err)
	}
	return created, nil
}

// RunByRunID reads one terminal invocation by its deterministic run id, the
// read model's single-row answer beside RunsByFunction's history. The
// function predicate stays alongside the run id — defense in depth is cheap,
// and a run of another function must be indistinguishable from a run that
// does not exist.
func (r *FunctionRunRepository) RunByRunID(ctx context.Context, tenantID string, functionID int64, runID string) (function.FunctionRun, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return function.FunctionRun{}, false, fmt.Errorf("begin function run get tx: %w", err)
	}
	// Unconditional release: the scan below captures its error into scanErr,
	// so the not-found and scan-failure returns leave err nil and a rollback
	// keyed on err would leak the transaction holding the tenant GUC.
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return function.FunctionRun{}, false, fmt.Errorf("set tenant context: %w", err)
	}

	var (
		run                   function.FunctionRun
		startedAt, finishedAt sql.NullTime
		durationMs            sql.NullInt64
	)
	scanErr := tx.QueryRowContext(ctx, `
		SELECT `+functionRunReadColumns+`
		FROM function_runs fr
		WHERE fr.tenant_id = $1 AND fr.function_id = $2 AND fr.run_id = $3
	`, tenantID, functionID, runID).Scan(
		&run.ID, &run.RunID, &run.TenantID, &run.FunctionID, &run.Status, &run.TriggeredAt,
		&startedAt, &finishedAt, &durationMs, &run.Version, &run.CreatedAt, &run.OccurrenceKey,
	)
	if scanErr == sql.ErrNoRows {
		// Deleted, foreign-function and other-tenant rows are alike not found:
		// RLS hides the last, the predicates hide the first two.
		return function.FunctionRun{}, false, nil
	}
	if scanErr != nil {
		return function.FunctionRun{}, false, fmt.Errorf("get function run: %w", scanErr)
	}
	if startedAt.Valid {
		started := startedAt.Time
		run.StartedAt = &started
	}
	if finishedAt.Valid {
		finished := finishedAt.Time
		run.FinishedAt = &finished
	}
	if durationMs.Valid {
		ms := int(durationMs.Int64)
		run.DurationMs = &ms
	}

	if err = tx.Commit(); err != nil {
		return function.FunctionRun{}, false, fmt.Errorf("commit function run get: %w", err)
	}
	return run, true, nil
}

// RunsByFunction lists the function's most recent terminal invocations, newest
// first. A non-positive limit means the default page.
func (r *FunctionRunRepository) RunsByFunction(ctx context.Context, tenantID string, functionID int64, limit int) ([]function.FunctionRun, error) {
	return r.RunsByFunctionPage(ctx, tenantID, functionID, limit, 0, "", "")
}

// RunsByFunctionPage filters terminal history on triggered_at before pagination.
func (r *FunctionRunRepository) RunsByFunctionPage(ctx context.Context, tenantID string, functionID int64, limit, offset int, from, before string) ([]function.FunctionRun, error) {
	if limit < 1 {
		limit = defaultRunPage
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin function runs list tx: %w", err)
	}
	// Unconditional release: the scan and rows.Err checks below capture their
	// errors into shadowed locals, so a rollback keyed on err leaked the tx
	// mid-iteration.
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return nil, fmt.Errorf("set tenant context: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT `+functionRunReadColumns+`
		FROM function_runs fr
		WHERE fr.tenant_id = $1 AND fr.function_id = $2
 AND (NULLIF($5,'')::timestamptz IS NULL OR fr.triggered_at >= NULLIF($5,'')::timestamptz)
 AND (NULLIF($6,'')::timestamptz IS NULL OR fr.triggered_at < NULLIF($6,'')::timestamptz)
		ORDER BY fr.triggered_at DESC, fr.id DESC
		LIMIT $3 OFFSET $4
	`, tenantID, functionID, limit, offset, queryfilter.MicrosecondBound(from), queryfilter.MicrosecondBound(before))
	if err != nil {
		return nil, fmt.Errorf("list function runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var runs []function.FunctionRun
	for rows.Next() {
		var run function.FunctionRun
		var startedAt, finishedAt sql.NullTime
		var durationMs sql.NullInt64
		err := rows.Scan(
			&run.ID, &run.RunID, &run.TenantID, &run.FunctionID, &run.Status, &run.TriggeredAt,
			&startedAt, &finishedAt, &durationMs, &run.Version, &run.CreatedAt, &run.OccurrenceKey,
		)
		if err != nil {
			return nil, fmt.Errorf("scan function run: %w", err)
		}
		if startedAt.Valid {
			started := startedAt.Time
			run.StartedAt = &started
		}
		if finishedAt.Valid {
			finished := finishedAt.Time
			run.FinishedAt = &finished
		}
		if durationMs.Valid {
			ms := int(durationMs.Int64)
			run.DurationMs = &ms
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate function runs: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit function runs list: %w", err)
	}
	return runs, nil
}

// Resource strings were normalized at the management boundary; marshal cannot fail.
func nullableResourceJSON(resources *executionpolicy.Resources) any {
	if resources == nil {
		return nil
	}
	encoded, _ := json.Marshal(resources)
	return encoded
}

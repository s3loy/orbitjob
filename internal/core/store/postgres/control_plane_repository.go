package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/resource"
)

// ErrRevisionConflict reports that a ScheduledJob generation was reprojected with
// a different spec hash. The API server bumps generation on every spec change, so
// this means two writers raced on one identity, or a caller normalized the spec
// non-deterministically. Both are defects; refusing the write keeps revisions
// immutable and auditable.
var ErrRevisionConflict = errors.New("definition revision conflicts with stored generation")

// ErrAttemptConflict reports that an attempt number is already bound to a
// different Kubernetes Job name. That indicates two writers created competing
// Jobs for one attempt, which must surface rather than overwrite observation
// state.
var ErrAttemptConflict = errors.New("attempt already bound to a different kubernetes job")

// ErrNoAttempt reports that no attempt row owns the given Kubernetes Job name.
var ErrNoAttempt = errors.New("no attempt owns kubernetes job")

// ErrAttemptOwnership reports that a Kubernetes Job name was reused by an
// object whose immutable UID does not match the attempt that owns the name.
var ErrAttemptOwnership = errors.New("kubernetes job uid does not own attempt")

// ErrRunChanged means the caller's attempt no longer matches the locked run.
var ErrRunChanged = errors.New("run changed before attempt write")

// ErrWorkflowStopped distinguishes durable parent stop intent from a transient
// child race, so the runtime converges through cancellation instead of spinning.
var ErrWorkflowStopped = fmt.Errorf("workflow no longer permits new work: %w", ErrRunChanged)

// ErrWorkflowBlocked quarantines uncertain work without implying cancellation.
var ErrWorkflowBlocked = fmt.Errorf("workflow execution is uncertain: %w", ErrRunChanged)

// ErrStaleObservation means a newer or terminal attempt observation is stored.
var ErrStaleObservation = errors.New("stale kubernetes job observation")

// sourceModeKubernetes is the source a ScheduledJob CR is projected from.
// Check definitions materialize under the check domain's SourceModeCheck; the
// revision-sourced listings read each execution source (see revisionSourceModes).
const sourceModeKubernetes = "kubernetes"

// Audit vocabulary for the run ledger.
//
// The event and resource types are VARCHAR(32) columns with no CHECK, so a typo
// would silently open a category of ledger change that no query looks for.
// Naming them here keeps the set closed. core may not import the admin store's
// helper, so the row is written inline, the way ADR 0005 rules for every layer
// that opens its own transaction.
const (
	auditEventRevisionProjected    = "definition_revision.projected"
	auditEventRunCreated           = "job_run.created"
	auditEventRunStatusChanged     = "job_run.status_changed"
	auditEventRunPruned            = "job_run.pruned"
	auditEventAttemptCreated       = "job_run_attempt.created"
	auditEventAttemptStatusChanged = "job_run_attempt.status_changed"

	auditResourceRevision = "job_definition_revision"
	auditResourceRun      = "job_run"
	auditResourceAttempt  = "job_run_attempt"
)

// maxActorLength is the width of audit_events.actor_id, the narrowest column
// that has to hold an actor. A longer actor is refused at the boundary rather
// than allowed to fail the audit insert later and take the change down with it.
const maxActorLength = 64

// ControlPlaneRepository owns the Kubernetes-native control plane tables:
// definition revisions, runs and attempts.
//
// Every write runs inside a tenant-scoped transaction so the RLS policies in the
// baseline bind, and records itself in audit_events in that same transaction,
// because a ledger whose changes cannot be attributed is not the record the
// product sells.
//
// Reads go through the same helper. They used to borrow a pooled connection and
// set the tenant GUC at session scope, which leaked: database/sql returns a
// connection to the pool without resetting session state, so the next borrower
// could run an unscoped query with the previous tenant's id still set and read
// another tenant's rows through RLS. A transaction-scoped setting cannot
// outlive its transaction.
type ControlPlaneRepository struct {
	db  *sql.DB
	now func() time.Time
}

func NewControlPlaneRepository(db *sql.DB) *ControlPlaneRepository {
	return &ControlPlaneRepository{db: db, now: time.Now}
}

// validateActor refuses an actor the ledger cannot record.
//
// job_run_control_plane rejects an empty actor in the schema, so refusing it here
// only moves the failure to where a caller can read it. The length limit comes
// from audit_events.actor_id: a write whose audit row cannot be inserted
// aborts the change, and doing that for a caller's long string would be a
// surprise rather than a bug report.
func validateActor(actor string) error {
	if strings.TrimSpace(actor) == "" {
		return fmt.Errorf("actor is required")
	}
	if utf8.RuneCountInString(actor) > maxActorLength {
		return fmt.Errorf("actor is longer than %d characters", maxActorLength)
	}
	return nil
}

// insertControlPlaneAudit appends one ledger-change row using the caller's
// transaction, so the change and its record commit together (ADR 0005).
//
// Control-plane processes retain system attribution. A direct human request
// carries verified user context; later operator writes retain their writer type.
func insertControlPlaneAudit(
	ctx context.Context, tx *sql.Tx,
	tenantID, actor, eventType, resourceType, resourceID string, diff map[string]any,
) error {
	if diff == nil {
		diff = map[string]any{}
	}
	encoded, err := json.Marshal(diff)
	if err != nil {
		return fmt.Errorf("encode audit diff: %w", err)
	}
	query := `
		INSERT INTO audit_events
			(tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
		VALUES ($1, 'system', NULLIF($2, ''), $3, $4, $5, $6::jsonb)
	`
	if _, err := tx.ExecContext(ctx, query, tenantID, actor, eventType, resourceType, resourceID, string(encoded)); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Definition revisions
// ---------------------------------------------------------------------------

// resource_group_id records the isolation tier the revision's source belongs
// to: a check-sourced revision inherits its check row's group, a ScheduledJob
// revision has none. Empty travels as NULL via nullableGroup, matching the
// nullable CHAR(26) the group-scoped tables (checks, slis, slos) carry.
const applyRevisionSQL = `
INSERT INTO job_definition_revisions
  (tenant_id, source_mode, source_uid, source_namespace, source_name,
   generation, spec_hash, normalized_spec, actor, is_active, resource_group_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,false,$10)
ON CONFLICT (source_mode, source_uid, generation) DO NOTHING
RETURNING id`

const selectRevisionIdentitySQL = `
SELECT id, spec_hash FROM job_definition_revisions
WHERE source_mode=$1 AND source_uid=$2 AND generation=$3 AND tenant_id=$4`

const lockRevisionSQL = `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`

const deactivateRevisionsSQL = `
UPDATE job_definition_revisions SET is_active=false
WHERE source_mode=$1 AND source_uid=$2 AND tenant_id=$3 AND is_active AND id <> $4
 AND generation <= (SELECT generation FROM job_definition_revisions WHERE id=$4 AND tenant_id=$3)`

const activateRevisionSQL = `
UPDATE job_definition_revisions r SET is_active=true,activated_at=COALESCE(activated_at,clock_timestamp()) WHERE id=$1 AND tenant_id=$2 AND NOT is_active
 AND NOT EXISTS (SELECT 1 FROM job_definition_revisions newer
 WHERE newer.tenant_id=r.tenant_id AND newer.source_mode=r.source_mode AND newer.source_uid=r.source_uid AND newer.generation>r.generation)`

// ApplyRevisionForTenant projects one ScheduledJob generation into an immutable
// revision and atomically switches the active pointer. Reprojecting the same
// generation with the same spec hash is idempotent; the same generation with a
// different hash returns ErrRevisionConflict and rolls the transaction back.
//
// actor is the identity that produced the projection. It is required: a revision
// nobody can attribute is a definition change the audit trail cannot explain.
func (r *ControlPlaneRepository) ApplyRevisionForTenant(ctx context.Context, tenantID string, rev revision.Revision) (int64, error) {
	if rev.Identity.SourceMode == "" || rev.Identity.SourceUID == "" {
		return 0, fmt.Errorf("revision identity is required")
	}
	if err := validateActor(rev.Actor); err != nil {
		return 0, err
	}

	var (
		id      int64
		created bool
	)
	err := withAdmissionTx(ctx, r.db, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		// A definition can have no row yet. An advisory transaction lock also
		// serializes that first generation against concurrent projections.
		if _, err := tx.ExecContext(ctx, lockRevisionSQL, sourceLockKey(tenantID, rev.Identity.SourceMode, rev.Identity.SourceUID)); err != nil {
			return err
		}
		control, _, err := readSourceControl(ctx, tx, tenantID, rev.Identity.SourceMode, rev.Identity.SourceUID)
		if err != nil {
			return err
		}
		if control.Tombstoned {
			return &jobrun.AdmissionBlockedError{Reason: "source_deleted"}
		}
		insertErr := tx.QueryRowContext(ctx, applyRevisionSQL,
			tenantID, rev.Identity.SourceMode, rev.Identity.SourceUID,
			rev.Identity.Namespace, rev.Identity.Name, rev.Generation,
			rev.SpecHash, rev.NormalizedSpec, rev.Actor, nullableGroup(rev.ResourceGroupID),
		).Scan(&id)
		switch {
		case errors.Is(insertErr, sql.ErrNoRows):
			// This generation was projected before. Read it back and refuse a
			// different hash: revisions are immutable, so the same generation
			// cannot describe two specs.
			var storedHash string
			if scanErr := tx.QueryRowContext(ctx, selectRevisionIdentitySQL,
				rev.Identity.SourceMode, rev.Identity.SourceUID, rev.Generation, tenantID,
			).Scan(&id, &storedHash); scanErr != nil {
				return fmt.Errorf("read projected revision: %w", scanErr)
			}
			if storedHash != rev.SpecHash {
				return ErrRevisionConflict
			}
		case insertErr != nil:
			return insertErr
		default:
			created = true
		}

		// Deactivate before activate. ux_job_definition_revisions_active is
		// unique on (source_mode, source_uid) WHERE is_active, and PostgreSQL
		// checks it per row, so a single statement that moved the pointer could
		// be rejected for a duplicate that only exists mid-statement.
		deactivated, execErr := tx.ExecContext(ctx, deactivateRevisionsSQL,
			rev.Identity.SourceMode, rev.Identity.SourceUID, tenantID, id)
		if execErr != nil {
			return execErr
		}
		previousActive, execErr := deactivated.RowsAffected()
		if execErr != nil {
			return execErr
		}
		activated, execErr := tx.ExecContext(ctx, activateRevisionSQL, id, tenantID)
		if execErr != nil {
			return execErr
		}
		currentActive, execErr := activated.RowsAffected()
		if execErr != nil {
			return execErr
		}

		// A replay of the revision that is already active changes nothing, and a
		// record of nothing is noise in a trail that is read by volume.
		if !created && previousActive == 0 && currentActive == 0 {
			return nil
		}
		return insertControlPlaneAudit(ctx, tx, tenantID, rev.Actor, auditEventRevisionProjected,
			auditResourceRevision, strconv.FormatInt(id, 10), map[string]any{
				"source_uid":       rev.Identity.SourceUID,
				"generation":       rev.Generation,
				"spec_hash":        rev.SpecHash,
				"revision_created": created,
			})
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

const revisionColumns = `
id, source_mode, source_uid, source_namespace, source_name,
generation, spec_hash, normalized_spec::text, actor, created_at`

func scanRevision(row *sql.Row) (revision.Revision, error) {
	var out revision.Revision
	err := row.Scan(
		&out.ID, &out.Identity.SourceMode, &out.Identity.SourceUID,
		&out.Identity.Namespace, &out.Identity.Name, &out.Generation,
		&out.SpecHash, &out.NormalizedSpec, &out.Actor, &out.CreatedAt,
	)
	return out, err
}

// RevisionByID loads the immutable definition a run was pinned to. Loading by id
// rather than by name is what keeps an in-flight run executing the spec it was
// created with even after the ScheduledJob is edited.
func (r *ControlPlaneRepository) RevisionByID(ctx context.Context, tenantID string, id int64) (revision.Revision, error) {
	var out revision.Revision
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		rev, scanErr := scanRevision(tx.QueryRowContext(ctx,
			`SELECT `+revisionColumns+` FROM job_definition_revisions WHERE id=$1 AND tenant_id=$2`, id, tenantID))
		if errors.Is(scanErr, sql.ErrNoRows) {
			return &resource.NotFoundError{Resource: "job_definition_revision", ID: id}
		}
		if scanErr != nil {
			return fmt.Errorf("revision %d: %w", id, scanErr)
		}
		out = rev
		return nil
	})
	if err != nil {
		return revision.Revision{}, err
	}
	return out, nil
}

// ActiveRevision returns the revision new runs must use for a definition.
func (r *ControlPlaneRepository) ActiveRevision(ctx context.Context, tenantID, sourceUID string) (revision.Revision, error) {
	var out revision.Revision
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		rev, scanErr := scanRevision(tx.QueryRowContext(ctx,
			`SELECT `+revisionColumns+` FROM job_definition_revisions
			 WHERE source_mode=$1 AND source_uid=$2 AND tenant_id=$3 AND is_active`,
			sourceModeKubernetes, sourceUID, tenantID))
		if errors.Is(scanErr, sql.ErrNoRows) {
			return &resource.NotFoundError{Resource: "job_definition_revision", ID: sourceUID}
		}
		if scanErr != nil {
			return fmt.Errorf("active revision for %s: %w", sourceUID, scanErr)
		}
		out = rev
		return nil
	})
	if err != nil {
		return revision.Revision{}, err
	}
	return out, nil
}

// ActiveFunctionRevision reads the active revision a function invocation pins:
// the job_definition_revisions row at (source_mode='function', source_uid,
// is_active), which the operator's revision-sync loop materializes from the
// function's current version. Identity and execution fields come from the same
// immutable row, including the timeout when the current definition is newer.
// A definition whose sync has not run
// yet has no row: that is NotFoundError, the caller's "no active revision"
// answer, and re-asking after the next sync tick is the designed recovery.
func (r *ControlPlaneRepository) ActiveFunctionRevision(ctx context.Context, tenantID, sourceUID string) (revision.Revision, error) {
	var out revision.Revision
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		rev, scanErr := scanRevision(tx.QueryRowContext(ctx,
			`SELECT `+revisionColumns+` FROM job_definition_revisions
			 WHERE source_mode=$1 AND source_uid=$2 AND tenant_id=$3 AND is_active`,
			function.SourceModeFunction, sourceUID, tenantID))
		if errors.Is(scanErr, sql.ErrNoRows) {
			return &resource.NotFoundError{Resource: "function_definition_revision", ID: sourceUID}
		}
		if scanErr != nil {
			return fmt.Errorf("active function revision for %s: %w", sourceUID, scanErr)
		}
		out = rev
		return nil
	})
	if err != nil {
		return revision.Revision{}, err
	}
	return out, nil
}

const activeRevisionsSQL = `
SELECT ` + revisionColumns + `
FROM job_definition_revisions
WHERE is_active AND source_mode = ANY($2) AND tenant_id=$1
ORDER BY source_uid`

// revisionSourceModes lists ordinary execution sources. Checks use their own
// cursor and functions are invoke-only, so both have an empty cron schedule;
// listing them allows run recovery and retention without scheduling new work.
func revisionSourceModes() []string {
	return []string{sourceModeKubernetes, check.SourceModeCheck, function.SourceModeFunction}
}

// ActiveRevisions lists every definition the scheduler may fire for a tenant.
// Only the active revision of each identity is returned: superseded revisions
// stay readable for audit but never schedule new work.
func (r *ControlPlaneRepository) ActiveRevisions(ctx context.Context, tenantID string) ([]revision.Revision, error) {
	var out []revision.Revision
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		rows, queryErr := tx.QueryContext(ctx, activeRevisionsSQL, tenantID, revisionSourceModes())
		if queryErr != nil {
			return queryErr
		}
		defer func() { _ = rows.Close() }()
		revs, scanErr := scanRevisions(rows)
		out = revs
		return scanErr
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// activeWorkflowRevisionsSQL is the workflow history retainer's sweep input:
// the active revision of every workflow-sourced definition, and nothing else.
const activeWorkflowRevisionsSQL = `
SELECT ` + revisionColumns + `
FROM job_definition_revisions
WHERE is_active AND source_mode = $2 AND tenant_id=$1
ORDER BY source_uid`

// ActiveWorkflowRevisions lists the tenant's active workflow revisions -- the
// one listing workflow-sourced rows in the store. ActiveRevisions deliberately
// stops short of them because ordinary Job scheduling decodes a different
// spec. Workflow scheduling and retention share this narrower source instead.
func (r *ControlPlaneRepository) ActiveWorkflowRevisions(ctx context.Context, tenantID string) ([]revision.Revision, error) {
	var out []revision.Revision
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		rows, queryErr := tx.QueryContext(ctx, activeWorkflowRevisionsSQL, tenantID, workflow.SourceModeWorkflow)
		if queryErr != nil {
			return queryErr
		}
		defer func() { _ = rows.Close() }()
		revs, scanErr := scanRevisions(rows)
		out = revs
		return scanErr
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// scanRevisions drains a job_definition_revisions result set in the
// revisionColumns order both active-revision listings select.
func scanRevisions(rows *sql.Rows) ([]revision.Revision, error) {
	var out []revision.Revision
	for rows.Next() {
		var rev revision.Revision
		if scanErr := rows.Scan(
			&rev.ID, &rev.Identity.SourceMode, &rev.Identity.SourceUID,
			&rev.Identity.Namespace, &rev.Identity.Name, &rev.Generation,
			&rev.SpecHash, &rev.NormalizedSpec, &rev.Actor, &rev.CreatedAt,
		); scanErr != nil {
			return nil, scanErr
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Runs
// ---------------------------------------------------------------------------

// resource_group_id is stamped from the pinned revision, not from the caller:
// a check-sourced revision carries its check row's group, so every run of that
// definition reports the tier its definition was created under. ScheduledJob
// revisions have no group and stamp NULL. The subquery reuses $3 and $1, so
// the statement takes no additional bind arguments.
//
// workflow_run_id ($10) is the step-grouping pointer. Plain occurrences pass
// NULL -- the column is nullable and the definition-retention sweep counts the
// NULL side as ordinary history -- and the workflow store's step creation is
// the only writer that stamps it, because "which workflow ordered this run" is
// a fact about the caller, not something this repository can derive.
const insertOccurrenceSQL = `
INSERT INTO job_run_control_plane
  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor,
   phase, attempt, max_attempts, scheduled_for, resource_group_id, workflow_run_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,0,$8,$9,
  (SELECT resource_group_id FROM job_definition_revisions WHERE id=$3 AND tenant_id=$1),
  $10)
ON CONFLICT (source_uid, occurrence_key) DO NOTHING
RETURNING id`

const selectOccurrenceSQL = `
SELECT id, revision_id, phase, attempt, max_attempts, scheduled_for,
 COALESCE((SELECT a.kubernetes_job_uid FROM job_run_attempts_control_plane a
 WHERE a.run_id=job_run_control_plane.id AND a.attempt_number=job_run_control_plane.attempt),''),
 COALESCE((SELECT a.kubernetes_job_name FROM job_run_attempts_control_plane a
 WHERE a.run_id=job_run_control_plane.id AND a.attempt_number=job_run_control_plane.attempt),'')
FROM job_run_control_plane WHERE source_uid=$1 AND occurrence_key=$2 AND tenant_id=$3`

func scanStoredRun(ctx context.Context, tx *sql.Tx, tenantID, sourceUID, occurrenceKey string) (jobrun.StoredRun, error) {
	var (
		out          jobrun.StoredRun
		phase        string
		scheduledFor sql.NullTime
	)
	err := tx.QueryRowContext(ctx, selectOccurrenceSQL, sourceUID, occurrenceKey, tenantID).
		Scan(&out.ID, &out.RevisionID, &phase, &out.Attempt, &out.MaxAttempts, &scheduledFor, &out.AttemptUID, &out.AttemptJobName)
	if err != nil {
		return jobrun.StoredRun{}, err
	}
	out.Phase = jobrun.Phase(phase)
	if scheduledFor.Valid {
		out.ScheduledFor = scheduledFor.Time
	}
	return out, nil
}

// CreateOccurrenceForTenant records a run unless the occurrence already exists,
// and always returns the stored state. Deduplication lives in the database, not
// the caller: two schedulers racing on one occurrence must converge on a single
// run, and the loser needs the winner's id to reconcile status.
//
// actor names whoever asked for the run -- a manual trigger's caller, or the
// scheduler identity for a tick. It is required and recorded on the run, which
// is the only place the ledger can answer "who triggered this" after the fact.
//
// The run always starts in Pending; a caller-supplied phase is refused rather
// than ignored, so a run cannot be created already finished.
func (r *ControlPlaneRepository) CreateOccurrenceForTenant(
	ctx context.Context, tenantID string, run jobrun.JobRun, maxAttempts int, actor string,
) (jobrun.StoredRun, bool, error) {
	return r.createOccurrence(ctx, tenantID, run, maxAttempts, actor, false)
}

// CancelUnadmittedOccurrenceForTenant records only a verified never-executed
// CR-first request. The caller must establish absence of execution. An existing
// row is returned unchanged so its ordinary cancellation guards remain in force.
func (r *ControlPlaneRepository) CancelUnadmittedOccurrenceForTenant(
	ctx context.Context, tenantID string, run jobrun.JobRun, maxAttempts int, actor string,
) (jobrun.StoredRun, bool, error) {
	if run.Trigger != jobrun.Manual && run.Trigger != jobrun.Function {
		return jobrun.StoredRun{}, false, fmt.Errorf("only CR-first requests can be canceled before admission")
	}
	return r.createOccurrence(ctx, tenantID, run, maxAttempts, actor, true)
}

func (r *ControlPlaneRepository) createOccurrence(
	ctx context.Context, tenantID string, run jobrun.JobRun, maxAttempts int, actor string, canceled bool,
) (jobrun.StoredRun, bool, error) {
	if run.SourceUID == "" || run.OccurrenceKey == "" {
		return jobrun.StoredRun{}, false, fmt.Errorf("run identity is required")
	}
	if run.Phase != "" && run.Phase != jobrun.Pending {
		return jobrun.StoredRun{}, false, fmt.Errorf("a run is created in %s, not %s", jobrun.Pending, run.Phase)
	}
	if err := validateActor(actor); err != nil {
		return jobrun.StoredRun{}, false, err
	}
	revisionID, err := strconv.ParseInt(run.RevisionID, 10, 64)
	if err != nil {
		return jobrun.StoredRun{}, false, fmt.Errorf("revision id %q is not an id: %w", run.RevisionID, err)
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var (
		state   jobrun.StoredRun
		created bool
	)
	err = withAdmissionTx(ctx, r.db, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		mode, lockErr := lockAdmissionSource(ctx, tx, tenantID, run.SourceUID, revisionID, "")
		if lockErr != nil {
			return lockErr
		}
		existing, findErr := scanStoredRun(ctx, tx, tenantID, run.SourceUID, run.OccurrenceKey)
		if findErr == nil {
			state = existing
			return nil
		}
		if !errors.Is(findErr, sql.ErrNoRows) {
			return findErr
		}
		if !canceled {
			if err := checkSourceAdmission(ctx, tx, tenantID, mode, run.SourceUID, run.Trigger); err != nil {
				return err
			}
			if !run.ScheduledFor.IsZero() {
				var eligible bool
				if err := tx.QueryRowContext(ctx, `SELECT is_active AND COALESCE(activated_at,created_at) <= $3 FROM job_definition_revisions WHERE id=$1 AND tenant_id=$2`, revisionID, tenantID, run.ScheduledFor.UTC()).Scan(&eligible); err != nil {
					return err
				}
				if !eligible {
					return &jobrun.AdmissionBlockedError{Reason: "before_revision_activation"}
				}
			}
		}
		phase := jobrun.Pending
		if canceled {
			phase = jobrun.Canceled
		}
		var scheduledFor any
		if !run.ScheduledFor.IsZero() {
			scheduledFor = run.ScheduledFor.UTC()
		}
		var id int64
		insertErr := tx.QueryRowContext(ctx, insertOccurrenceSQL,
			tenantID, run.SourceUID, revisionID, run.OccurrenceKey,
			string(run.Trigger), actor, string(phase), maxAttempts,
			scheduledFor,
			nil, // a plain occurrence is nobody's step: the pointer stays NULL
		).Scan(&id)
		switch {
		case errors.Is(insertErr, sql.ErrNoRows):
			// The occurrence is already recorded; this writer lost the race.
		case insertErr != nil:
			return insertErr
		default:
			created = true
		}

		stored, scanErr := scanStoredRun(ctx, tx, tenantID, run.SourceUID, run.OccurrenceKey)
		if scanErr != nil {
			return fmt.Errorf("read occurrence: %w", scanErr)
		}
		state = stored
		if !created {
			return nil
		}
		return insertControlPlaneAudit(ctx, tx, tenantID, actor, auditEventRunCreated,
			auditResourceRun, strconv.FormatInt(stored.ID, 10), map[string]any{
				"source_uid":     run.SourceUID,
				"revision_id":    revisionID,
				"occurrence_key": run.OccurrenceKey,
				"trigger":        string(run.Trigger),
			})
	})
	recordAdmission(ctx, tenantID, run.SourceUID, created, err)
	if err != nil {
		return jobrun.StoredRun{}, false, err
	}
	return state, created, nil
}

// RunByOccurrence loads stored progress for the run identified by its definition
// UID and occurrence key. The controller resolves run state here rather than from
// the JobRun status subresource, so a forged or stale status cannot change what
// actually executes.
func (r *ControlPlaneRepository) RunByOccurrence(
	ctx context.Context, tenantID, sourceUID, occurrenceKey string,
) (jobrun.StoredRun, bool, error) {
	var (
		state jobrun.StoredRun
		found = true
	)
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		stored, scanErr := scanStoredRun(ctx, tx, tenantID, sourceUID, occurrenceKey)
		if errors.Is(scanErr, sql.ErrNoRows) {
			found = false
			return nil
		}
		if scanErr != nil {
			return scanErr
		}
		state = stored
		return nil
	})
	if err != nil {
		return jobrun.StoredRun{}, false, err
	}
	return state, found, nil
}

// updateRunPhaseSQL moves a run to a new phase and reports the phase it came
// from, plus the run's actor for the audit row.
//
// A terminal run is final. The phase predicate keeps a late observation of an
// old Kubernetes Job from dragging a finished run back into the lifecycle:
// nothing deletes Job objects, so the informer re-delivers them and an unguarded
// write would make the run oscillate with every resync.
//
// The observation must belong to the current attempt and move forward. Only
// CreateAttemptForTenant may advance the counter, in its attempt transaction.
// Cancellation is authoritative over an old Job's ordinary observations.
const updateRunPhaseSQL = `
WITH prior AS (
  SELECT phase AS prior_phase, attempt AS prior_attempt, actor AS run_actor
  FROM job_run_control_plane
  WHERE id=$1 AND tenant_id=$2
  FOR UPDATE
)
UPDATE job_run_control_plane r
SET phase=$3, attempt=$4, updated_at=$5
FROM prior
WHERE r.id=$1 AND r.tenant_id=$2
  AND (r.phase <> $3 OR r.attempt <> $4)
  AND NOT (r.phase = ANY($6))
  AND r.attempt=$4
  AND ($3 NOT IN ('Succeeded','Failed','RetryWaiting','Canceled') OR NOT EXISTS (
    SELECT 1 FROM job_run_attempts_control_plane a WHERE a.tenant_id=r.tenant_id AND a.run_id=r.id
    AND ` + pendingEvidenceSQL + `))
  AND ((r.phase='Pending' AND ($3='CancelRequested' OR ($3='Canceled' AND r.attempt=0)))
    OR (r.phase='CreatingAttempt' AND $3 IN ('Running','RetryWaiting','Succeeded','Failed','CancelRequested'))
    OR (r.phase='Running' AND $3 IN ('RetryWaiting','Succeeded','Failed','CancelRequested'))
    OR (r.phase='RetryWaiting' AND $3 IN ('Failed','CancelRequested'))
    OR (r.phase='ExecutionUnknown' AND $3 IN ('Running','RetryWaiting','Succeeded','CancelRequested'))
    OR (r.phase='CancelRequested' AND $3 IN ('Canceled','CancelUnknown'))
    OR (r.phase='CancelUnknown' AND $3='Canceled'))
RETURNING r.phase, prior.prior_phase, prior.prior_attempt, prior.run_actor`

const selectRunProgressSQL = `SELECT phase, attempt FROM job_run_control_plane WHERE id=$1 AND tenant_id=$2`

// UpdateRunPhase advances the current attempt's run phase. It reports whether this write
// changed the run's phase: a resync that observes what is already stored must
// not re-fire the terminal-phase bookkeeping (the check read model, SLI
// snapshots, metrics) that only a real transition earns.
//
// A run that already finished returns jobrun.ErrRunTerminal; a write that
// changes neither phase nor attempt returns (false, nil), because a reconcile
// that observed nothing new is not an error and not a change worth recording.
func (r *ControlPlaneRepository) UpdateRunPhase(
	ctx context.Context, tenantID string, runID int64, phase jobrun.Phase, attempt int,
) (bool, error) {
	changed := false
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		var parent sql.NullInt64
		finishing := attempt > 0 && (jobrun.Terminal(phase) || phase == jobrun.RetryWaiting)
		if finishing || phase == jobrun.CancelUnknown {
			var err error
			parent, err = lockWorkflowParentForObservation(ctx, tx, tenantID, runID)
			if err != nil {
				return err
			}
		}
		if finishing {
			// Acquire the run before the phase statement takes its snapshot.
			// Otherwise a pending proof committed while waiting for its lock
			// could be invisible to that statement's retention predicate.
			var lockedID int64
			if err := tx.QueryRowContext(ctx, `SELECT id FROM job_run_control_plane WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, runID).Scan(&lockedID); err != nil {
				return err
			}
		}
		if phase == jobrun.Canceled && attempt > 0 {
			if err := requireCancellationEvidence(ctx, tx, tenantID, runID, attempt); err != nil {
				return err
			}
		}
		var (
			newPhase     string
			priorPhase   string
			priorAttempt int
			runActor     string
		)
		scanErr := tx.QueryRowContext(ctx, updateRunPhaseSQL,
			runID, tenantID, string(phase), attempt, r.now().UTC(), terminalPhases(),
		).Scan(&newPhase, &priorPhase, &priorAttempt, &runActor)
		if scanErr == nil {
			changed = priorPhase != newPhase
			if phase == jobrun.CancelUnknown {
				if err := isolateWorkflowForUnknown(ctx, tx, tenantID, parent, r.now().UTC()); err != nil {
					return err
				}
			}
			return insertControlPlaneAudit(ctx, tx, tenantID, runActor, auditEventRunStatusChanged,
				auditResourceRun, strconv.FormatInt(runID, 10), map[string]any{
					"from_phase":   priorPhase,
					"to_phase":     newPhase,
					"from_attempt": priorAttempt,
					"to_attempt":   attempt,
				})
		}
		if !errors.Is(scanErr, sql.ErrNoRows) {
			return scanErr
		}

		// The write was refused. Read why: a run that already holds the state
		// the caller asked for is done, a terminal run is final, a missing run
		// is not this tenant's.
		var (
			current        string
			currentAttempt int
		)
		readErr := tx.QueryRowContext(ctx, selectRunProgressSQL, runID, tenantID).
			Scan(&current, &currentAttempt)
		if errors.Is(readErr, sql.ErrNoRows) {
			return &resource.NotFoundError{Resource: "job_run", ID: runID}
		}
		if readErr != nil {
			return readErr
		}
		if current == string(phase) && currentAttempt == attempt {
			if phase == jobrun.CancelUnknown {
				return isolateWorkflowForUnknown(ctx, tx, tenantID, parent, r.now().UTC())
			}
			return nil
		}
		if jobrun.Terminal(jobrun.Phase(current)) {
			return jobrun.ErrRunTerminal
		}
		if attempt > currentAttempt {
			return ErrRunChanged
		}
		// Older attempts and backward observations are an expected informer
		// replay. They must not undo cancellation or a completed retry.
		return nil
	})
	return changed, err
}

const countOpenRunsSQL = `
SELECT count(*) FROM job_run_control_plane
WHERE tenant_id=$1 AND source_uid=$2 AND phase <> ALL($3)`

// CountOpenRuns reports how many runs for a definition have not reached a
// terminal phase. The Forbid concurrency policy uses it to skip rather than
// stack runs on top of a job that is still running.
func (r *ControlPlaneRepository) CountOpenRuns(ctx context.Context, tenantID, sourceUID string) (int, error) {
	var count int
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, countOpenRunsSQL, tenantID, sourceUID, terminalPhases()).Scan(&count)
	})
	return count, err
}

const openRunsSQL = `
SELECT id, revision_id, occurrence_key, phase, scheduled_for, trigger, actor
FROM job_run_control_plane
WHERE tenant_id=$1 AND source_uid=$2 AND phase <> ALL($3)
ORDER BY id`

// OpenRuns lists runs that have not reached a terminal phase. The scheduler uses
// it to repair runs whose Kubernetes representation is missing before applying
// concurrency policy, which those very runs would otherwise block.
func (r *ControlPlaneRepository) OpenRuns(ctx context.Context, tenantID, sourceUID string) ([]jobrun.OpenRun, error) {
	var out []jobrun.OpenRun
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		rows, queryErr := tx.QueryContext(ctx, openRunsSQL, tenantID, sourceUID, terminalPhases())
		if queryErr != nil {
			return queryErr
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var (
				run          jobrun.OpenRun
				phase        string
				scheduledFor sql.NullTime
			)
			if scanErr := rows.Scan(&run.ID, &run.RevisionID, &run.OccurrenceKey, &phase, &scheduledFor, &run.Trigger, &run.Actor); scanErr != nil {
				return scanErr
			}
			run.Phase = jobrun.Phase(phase)
			if scheduledFor.Valid {
				run.ScheduledFor = scheduledFor.Time
			}
			out = append(out, run)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// prunableRunsSQL ranks runs per outcome in one pass, so the kept history is
// decided by the database and the two keep limits can differ without the caller
// issuing a query per outcome.
//
// The sweep excludes workflow steps (workflow_run_id IS NULL): a step is an
// ordinary run of the referenced definition, but its history belongs to the
// workflow run that grouped it, and pruning it here would pull rows out from
// under a live workflow. Steps age out through the workflow prune's
// steps-then-row pass, which is the only writer allowed to delete them.
const prunableRunsSQL = `
SELECT id, occurrence_key, phase, revision_id, trigger FROM (
  SELECT id, occurrence_key, phase, revision_id, trigger,
         row_number() OVER (PARTITION BY phase ORDER BY created_at DESC, id DESC) AS rank,
         CASE WHEN phase = $4 THEN $5::int ELSE $6::int END AS keep
  FROM job_run_control_plane
  WHERE tenant_id=$1 AND source_uid=$2 AND phase = ANY($3) AND workflow_run_id IS NULL
    AND outcome_recorded_at IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM job_run_attempts_control_plane a
      WHERE a.tenant_id=job_run_control_plane.tenant_id AND a.run_id=job_run_control_plane.id
      AND ` + pendingEvidenceSQL + `)
    AND (updated_at<now()-interval '15 days' OR EXISTS (
      SELECT 1 FROM execution_log_archives e WHERE e.tenant_id=job_run_control_plane.tenant_id
      AND e.run_id=job_run_control_plane.id AND e.state IN ('expired','deleted')))
    AND NOT EXISTS (SELECT 1 FROM execution_log_archives e
      WHERE e.tenant_id=job_run_control_plane.tenant_id AND e.run_id=job_run_control_plane.id
      AND e.state NOT IN ('expired','deleted') AND (e.protected_until IS NULL OR e.protected_until>now()))
) ranked
WHERE rank > keep
ORDER BY id`

// Only verified terminal outcomes can release their history and source slot.
// Unknown execution or cancellation remains quarantined without a time limit.
func retentionPhases() []string {
	return terminalPhases()
}

// PrunableRuns lists runs beyond the retained history for a definition. Rows are
// ranked per outcome, so a burst of failures cannot evict the success history
// and vice versa. Only phases in retentionPhases are eligible, and a run that
// started executing between the scan and the delete is left alone by DeleteRun.
//
// Cancellation uncertainty is not a failure-retention outcome.
func (r *ControlPlaneRepository) PrunableRuns(
	ctx context.Context, tenantID, sourceUID string, keepSuccessful, keepFailed int,
) ([]jobrun.PrunableRun, error) {
	if keepSuccessful < 0 {
		keepSuccessful = 0
	}
	if keepFailed < 0 {
		keepFailed = 0
	}
	var out []jobrun.PrunableRun
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		rows, queryErr := tx.QueryContext(ctx, prunableRunsSQL,
			tenantID, sourceUID, retentionPhases(), string(jobrun.Succeeded), keepSuccessful, keepFailed)
		if queryErr != nil {
			return queryErr
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var (
				run   jobrun.PrunableRun
				phase string
			)
			if scanErr := rows.Scan(&run.ID, &run.OccurrenceKey, &phase, &run.RevisionID, &run.Trigger); scanErr != nil {
				return scanErr
			}
			run.Phase = jobrun.Phase(phase)
			out = append(out, run)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

const deleteRunSQL = `
DELETE FROM job_run_control_plane
WHERE id=$1 AND tenant_id=$2 AND phase = ANY($3) AND workflow_run_id IS NULL
 AND outcome_recorded_at IS NOT NULL
 AND NOT EXISTS (SELECT 1 FROM job_run_attempts_control_plane a
   WHERE a.tenant_id=job_run_control_plane.tenant_id AND a.run_id=job_run_control_plane.id
   AND ` + pendingEvidenceSQL + `)
 AND (updated_at<now()-interval '15 days' OR EXISTS (
   SELECT 1 FROM execution_log_archives e WHERE e.tenant_id=job_run_control_plane.tenant_id
   AND e.run_id=job_run_control_plane.id AND e.state IN ('expired','deleted')))
 AND NOT EXISTS (SELECT 1 FROM execution_log_archives e
   WHERE e.tenant_id=job_run_control_plane.tenant_id AND e.run_id=job_run_control_plane.id
   AND e.state NOT IN ('expired','deleted') AND (e.protected_until IS NULL OR e.protected_until>now()))
RETURNING occurrence_key, phase, actor`

// DeleteRun removes one pruned run and records the removal. The phase predicate
// is repeated in the statement so a run that started executing between the scan
// and the delete is left alone rather than silently dropped mid-flight; a run
// the statement did not touch is reported as not deleted rather than as an
// error, because retention will see it again on the next sweep. The
// workflow_run_id predicate keeps the single-run path out of the step ledger
// for the same reason the ranked listing carries it: steps go with their
// workflow run, or not at all.
func (r *ControlPlaneRepository) DeleteRun(ctx context.Context, tenantID string, runID int64) (bool, error) {
	var deleted bool
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		var lockedID int64
		lockErr := tx.QueryRowContext(ctx, `SELECT id FROM job_run_control_plane WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, runID, tenantID).Scan(&lockedID)
		if errors.Is(lockErr, sql.ErrNoRows) {
			return nil
		}
		if lockErr != nil {
			return lockErr
		}
		var (
			occurrenceKey string
			phase         string
			actor         string
		)
		scanErr := tx.QueryRowContext(ctx, deleteRunSQL, runID, tenantID, retentionPhases()).
			Scan(&occurrenceKey, &phase, &actor)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return nil
		}
		if scanErr != nil {
			return scanErr
		}
		if err := insertControlPlaneAudit(ctx, tx, tenantID, actor, auditEventRunPruned,
			auditResourceRun, strconv.FormatInt(runID, 10), map[string]any{
				"occurrence_key": occurrenceKey,
				"phase":          phase,
			}); err != nil {
			return err
		}
		deleted = true
		return nil
	})
	return deleted, err
}

// ---------------------------------------------------------------------------
// Attempts
// ---------------------------------------------------------------------------

const insertAttemptSQL = `
INSERT INTO job_run_attempts_control_plane
  (tenant_id, run_id, attempt_number, phase, kubernetes_job_name,
   kubernetes_job_uid, observed_resource_version, started_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (run_id, attempt_number) DO UPDATE
  SET kubernetes_job_name = job_run_attempts_control_plane.kubernetes_job_name
  WHERE job_run_attempts_control_plane.kubernetes_job_name = EXCLUDED.kubernetes_job_name
    AND (EXCLUDED.kubernetes_job_uid='' OR job_run_attempts_control_plane.kubernetes_job_uid = EXCLUDED.kubernetes_job_uid)
RETURNING id`

const advanceRunForAttemptSQL = `
UPDATE job_run_control_plane
SET phase=CASE WHEN attempt=$2 THEN phase ELSE $1 END, attempt=$2, updated_at=$3
WHERE id=$4 AND tenant_id=$5 AND NOT (phase = ANY($6))
  AND NOT EXISTS (SELECT 1 FROM job_run_attempts_control_plane a
    WHERE a.tenant_id=job_run_control_plane.tenant_id AND a.run_id=job_run_control_plane.id
    AND ` + pendingEvidenceSQL + `)
  AND ((phase IN ('Pending','RetryWaiting') AND attempt+1=$2 AND $2<=max_attempts)
    OR (phase IN ('CreatingAttempt','Running') AND attempt=$2))
RETURNING actor`

const lockRunForAttemptSQL = `SELECT id FROM job_run_control_plane WHERE id=$1 AND tenant_id=$2 FOR UPDATE`

// CreateAttemptForTenant records one platform attempt and advances its run in
// the same transaction.
//
// One transaction, because the two writes are one fact. The attempt row is the
// history of how many times this run executed; the run's attempt counter is what
// the next reconcile reads to decide the next attempt number. Committed apart, a
// crash in between leaves the attempts table ahead of the counter, so the next
// reconcile starts attempt N+1 for a Job that is still running -- one occurrence
// executing twice, which is the failure the whole attempt ledger exists to
// prevent.
//
// runPhase is the phase the run takes when the attempt is recorded. The caller
// decides it because only the caller knows whether this attempt is a first
// attempt or a retry.
func (r *ControlPlaneRepository) CreateAttemptForTenant(
	ctx context.Context, tenantID string, runID int64, attempt jobrun.Attempt, runPhase jobrun.Phase,
) error {
	return r.createAttemptForTenant(ctx, tenantID, runID, attempt, runPhase, false)
}

func (r *ControlPlaneRepository) createAttemptForTenant(ctx context.Context, tenantID string, runID int64, attempt jobrun.Attempt, runPhase jobrun.Phase, reservation bool) error {
	if attempt.Number < 1 || attempt.KubernetesJobName == "" || (attempt.KubernetesJobUID == "" && !reservation) || (runPhase != jobrun.CreatingAttempt && runPhase != jobrun.Running) {
		return fmt.Errorf("attempt identity is required")
	}
	return r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		keys, stage, err := lockRunExecutionSources(ctx, tx, tenantID, runID)
		if err != nil {
			return err
		}
		if stage != jobrun.DispatchMayHaveStarted && stage != jobrun.DispatchBound {
			if err := checkExecutionSources(ctx, tx, tenantID, keys); err != nil {
				return err
			}
		}
		if err := lockWorkflowParentForAttempt(ctx, tx, tenantID, runID); err != nil {
			return err
		}
		// Recovery and cancellation lock run before attempt. A replayed INSERT
		// can lock an existing attempt too, so acquire the run before that write
		// rather than waiting until advanceRunForAttemptSQL to reverse the order.
		var lockedRunID int64
		if err := tx.QueryRowContext(ctx, lockRunForAttemptSQL, runID, tenantID).Scan(&lockedRunID); err != nil {
			return err
		}
		var started any
		if !attempt.StartedAt.IsZero() {
			started = attempt.StartedAt.UTC()
		}
		var attemptID int64
		insertErr := tx.QueryRowContext(ctx, insertAttemptSQL,
			tenantID, runID, attempt.Number, string(attempt.Phase),
			attempt.KubernetesJobName, attempt.KubernetesJobUID,
			attempt.ObservedResourceVersion, started,
		).Scan(&attemptID)
		if errors.Is(insertErr, sql.ErrNoRows) {
			return ErrAttemptConflict
		}
		if insertErr != nil {
			return insertErr
		}

		var runActor string
		if attempt.OwnerRunUID != "" {
			bound, err := tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET owner_run_uid=COALESCE(owner_run_uid,$1) WHERE id=$2 AND tenant_id=$3 AND (owner_run_uid IS NULL OR owner_run_uid=$1)`, attempt.OwnerRunUID, attemptID, tenantID)
			if err != nil {
				return err
			}
			count, err := bound.RowsAffected()
			if err != nil {
				return err
			}
			if count != 1 {
				return ErrAttemptOwnership
			}
		}
		if err := tx.QueryRowContext(ctx, advanceRunForAttemptSQL,
			string(runPhase), attempt.Number, r.now().UTC(), runID, tenantID, terminalPhases(),
		).Scan(&runActor); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				var phase string
				var number int
				if readErr := tx.QueryRowContext(ctx, selectRunProgressSQL, runID, tenantID).Scan(&phase, &number); readErr != nil {
					return readErr
				}
				if jobrun.Terminal(jobrun.Phase(phase)) {
					return jobrun.ErrRunTerminal
				}
				return ErrRunChanged
			}
			return err
		}

		return insertControlPlaneAudit(ctx, tx, tenantID, runActor, auditEventAttemptCreated,
			auditResourceAttempt, strconv.FormatInt(attemptID, 10), map[string]any{
				"run_id":              runID,
				"attempt_number":      attempt.Number,
				"kubernetes_job_name": attempt.KubernetesJobName,
				"phase":               string(attempt.Phase),
			})
	})
}

// updateAttemptPhaseSQL writes an observed phase back onto the attempt and
// reports the run it belongs to, the phase it came from, and the run's actor.
//
// The row is locked before it is updated so the phase a caller saw is the phase
// the update is compared against; two observations of one Job arriving
// concurrently cannot both record the same transition.
//
// $6 stamps completed_at for a finished attempt. RetryWaiting represents an
// attempt that failed but whose logical run still has retries remaining.
// Identity binding can record the Job's RV before its phase is observed, and
// quarantine replaces the last observed phase without changing that RV. Only
// these incomplete observations may advance phase at the same numeric RV.
const updateAttemptPhaseSQL = `
WITH prior AS (
  SELECT id, run_id, attempt_number, phase AS prior_phase, kubernetes_job_uid, observed_resource_version
  FROM job_run_attempts_control_plane
  WHERE kubernetes_job_name=$1 AND tenant_id=$2
  FOR UPDATE
), updated AS (
  UPDATE job_run_attempts_control_plane a
  SET phase=$3,
      observed_resource_version = COALESCE(NULLIF($5,''), a.observed_resource_version),
      completed_at = CASE WHEN $6 AND a.completed_at IS NULL
                          THEN $7::timestamptz ELSE a.completed_at END
  FROM prior
  WHERE a.id=prior.id
    AND (NOT $6 OR NOT (` + pendingEvidenceSQL + `))
    AND (($3='Canceled' AND $4='') OR
         (NULLIF($4,'') IS NOT NULL AND prior.kubernetes_job_uid=$4))
    AND (prior.prior_phase=$3 OR prior.prior_phase NOT IN ('Succeeded','Failed','Canceled','RetryWaiting'))
    AND NOT (prior.prior_phase='Running' AND $3='CreatingAttempt')
    AND CASE WHEN $5 ~ '^[0-9]+$' AND prior.observed_resource_version ~ '^[0-9]+$'
      THEN $5::numeric > prior.observed_resource_version::numeric
        OR ($5::numeric = prior.observed_resource_version::numeric AND
          (prior.prior_phase=$3 OR
            (prior.prior_phase IN ('CreatingAttempt','ExecutionUnknown') AND
              $3 IN ('CreatingAttempt','Running','Succeeded','Failed','Canceled','RetryWaiting'))))
      ELSE TRUE END
  RETURNING a.id, a.run_id, a.attempt_number, prior.prior_phase, a.phase,
            (SELECT r.actor FROM job_run_control_plane r WHERE r.id = prior.run_id),
            TRUE AS ownership_match
)
SELECT * FROM updated
UNION ALL
SELECT prior.id, prior.run_id, prior.attempt_number, prior.prior_phase,
       NULL,
       (SELECT r.actor FROM job_run_control_plane r WHERE r.id = prior.run_id),
       (($3='Canceled' AND $4='') OR (NULLIF($4,'') IS NOT NULL AND prior.kubernetes_job_uid=$4)) AS ownership_match
FROM prior
WHERE NOT EXISTS (SELECT 1 FROM updated)`

// UpdateAttemptPhase writes an observed Kubernetes Job phase back onto the
// attempt and reports which run and attempt it belongs to. Terminal phases stamp
// completed_at exactly once, preserve terminal results and never turn an
// observed Running attempt back into creation. ErrNoAttempt means no attempt owns that
// Job name, so a Job left over from a previous installation is not mistaken for
// this attempt's. ErrAttemptOwnership is returned when the name matches but the
// observed immutable UID does not. The cancellation path is the sole exception:
// it updates after deleting the Job and therefore has no observed UID.
//
// A resync that observes the phase the attempt already has fills in observed
// identity but changes nothing the trail needs, so it records nothing: an audit
// row per informer resync would bury the transitions that matter.
func (r *ControlPlaneRepository) UpdateAttemptPhase(
	ctx context.Context, tenantID, jobName, phase, jobUID, resourceVersion string,
) (runID int64, attemptNumber int, err error) {
	if jobName == "" || phase == "" {
		return 0, 0, fmt.Errorf("job name and phase are required")
	}
	err = r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		var (
			attemptID   int64
			priorPhase  string
			newPhase    sql.NullString
			runActor    string
			ownsAttempt bool
		)
		scanErr := tx.QueryRowContext(ctx, updateAttemptPhaseSQL,
			jobName, tenantID, phase, jobUID, resourceVersion,
			jobrun.Terminal(jobrun.Phase(phase)) || phase == string(jobrun.RetryWaiting), r.now().UTC(),
		).Scan(&attemptID, &runID, &attemptNumber, &priorPhase, &newPhase, &runActor, &ownsAttempt)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return ErrNoAttempt
		}
		if scanErr != nil {
			return scanErr
		}
		if !ownsAttempt {
			return ErrAttemptOwnership
		}
		if !newPhase.Valid {
			return ErrStaleObservation
		}
		if priorPhase == newPhase.String {
			return nil
		}
		return insertControlPlaneAudit(ctx, tx, tenantID, runActor, auditEventAttemptStatusChanged,
			auditResourceAttempt, strconv.FormatInt(attemptID, 10), map[string]any{
				"run_id":         runID,
				"attempt_number": attemptNumber,
				"from_phase":     priorPhase,
				"to_phase":       newPhase.String,
			})
	})
	if err != nil {
		return 0, 0, err
	}
	return runID, attemptNumber, nil
}

// terminalPhases is the SQL form of jobrun.Terminal, which stays the authority
// on what a finished phase is. Statements that take it as a parameter compare a
// phase column against it; a statement that needs the answer for one value
// passes jobrun.Terminal directly.
func terminalPhases() []string {
	return []string{
		string(jobrun.Succeeded), string(jobrun.Failed), string(jobrun.Canceled),
	}
}

const terminalOutcomeSelectSQL = `
SELECT r.id, r.tenant_id, r.source_uid, r.occurrence_key, r.scheduled_for, r.created_at, r.phase, r.outcome_recorded_at,
       a.started_at, a.completed_at
FROM job_run_control_plane r
LEFT JOIN LATERAL (
  SELECT started_at, completed_at
  FROM job_run_attempts_control_plane a
  WHERE a.run_id = r.id
  ORDER BY a.attempt_number DESC
  LIMIT 1
) a ON true`

const terminalOutcomeSQL = `SELECT source_uid, occurrence_key, scheduled_for, created_at, phase, started_at, completed_at FROM (` + terminalOutcomeSelectSQL + `) r WHERE id=$1 AND tenant_id=$2`

// TerminalOutcome reads everything a terminal run's bookkeeping needs in one
// query: the definition it ran for, the occurrence it was for, its stable
// ledger creation instant, when it was scheduled, and the wall-clock span of
// its newest attempt. Derived bookkeeping is written outside the phase
// transition's transaction, so this read is its consistent view of that fact.
func (r *ControlPlaneRepository) TerminalOutcome(
	ctx context.Context, tenantID string, runID int64,
) (controlplane.TerminalOutcome, error) {
	var (
		out          controlplane.TerminalOutcome
		phase        string
		scheduledFor sql.NullTime
		startedAt    sql.NullTime
		completedAt  sql.NullTime
	)
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		scanErr := tx.QueryRowContext(ctx, terminalOutcomeSQL, runID, tenantID).
			Scan(&out.SourceUID, &out.OccurrenceKey, &scheduledFor, &out.CreatedAt, &phase, &startedAt, &completedAt)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return &resource.NotFoundError{Resource: "job_run", ID: runID}
		}
		if scanErr != nil {
			return fmt.Errorf("terminal outcome for run %d: %w", runID, scanErr)
		}
		out.RunID = runID
		out.Phase = jobrun.Phase(phase)
		if scheduledFor.Valid {
			out.ScheduledFor = scheduledFor.Time
		}
		if startedAt.Valid {
			out.StartedAt = startedAt.Time
		}
		if completedAt.Valid {
			out.CompletedAt = completedAt.Time
		}
		return nil
	})
	if err != nil {
		return controlplane.TerminalOutcome{}, err
	}
	return out, nil
}

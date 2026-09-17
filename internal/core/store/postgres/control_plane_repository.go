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
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
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

// sourceModeKubernetes is the source a ScheduledJob CR is projected from.
// Check definitions materialize under the check domain's SourceModeCheck; the
// revision-sourced listings read both (see revisionSourceModes).
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
// actor_type is 'system' because every writer of these tables is a control-plane
// process. The identity that asked for the work travels in actor_id and, for a
// run, in job_run_control_plane.actor.
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
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_events
			(tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
		VALUES ($1, 'system', NULLIF($2, ''), $3, $4, $5, $6::jsonb)
	`, tenantID, actor, eventType, resourceType, resourceID, string(encoded)); err != nil {
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

const deactivateRevisionsSQL = `
UPDATE job_definition_revisions SET is_active=false
WHERE source_mode=$1 AND source_uid=$2 AND tenant_id=$3 AND is_active AND id <> $4`

const activateRevisionSQL = `
UPDATE job_definition_revisions SET is_active=true WHERE id=$1 AND tenant_id=$2 AND NOT is_active`

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
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
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

const activeRevisionsSQL = `
SELECT ` + revisionColumns + `
FROM job_definition_revisions
WHERE is_active AND source_mode = ANY($2) AND tenant_id=$1
ORDER BY source_uid`

// revisionSourceModes lists the sources active revisions are listed for. Both
// the job scheduler and retention walk this list: a ScheduledJob revision
// carries its schedule in the spec, and a check revision synthesizes an empty
// one (its cursor is checks.next_run_at), which the job scheduler skips.
// Retention reads both so check runs age out like every other run.
func revisionSourceModes() []string {
	return []string{sourceModeKubernetes, check.SourceModeCheck}
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
		for rows.Next() {
			var rev revision.Revision
			if scanErr := rows.Scan(
				&rev.ID, &rev.Identity.SourceMode, &rev.Identity.SourceUID,
				&rev.Identity.Namespace, &rev.Identity.Name, &rev.Generation,
				&rev.SpecHash, &rev.NormalizedSpec, &rev.Actor, &rev.CreatedAt,
			); scanErr != nil {
				return scanErr
			}
			out = append(out, rev)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
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
SELECT id, revision_id, phase, attempt, max_attempts, scheduled_for
FROM job_run_control_plane WHERE source_uid=$1 AND occurrence_key=$2 AND tenant_id=$3`

func scanStoredRun(ctx context.Context, tx *sql.Tx, tenantID, sourceUID, occurrenceKey string) (jobrun.StoredRun, error) {
	var (
		out          jobrun.StoredRun
		phase        string
		scheduledFor sql.NullTime
	)
	err := tx.QueryRowContext(ctx, selectOccurrenceSQL, sourceUID, occurrenceKey, tenantID).
		Scan(&out.ID, &out.RevisionID, &phase, &out.Attempt, &out.MaxAttempts, &scheduledFor)
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
	err = r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		var scheduledFor any
		if !run.ScheduledFor.IsZero() {
			scheduledFor = run.ScheduledFor.UTC()
		}
		var id int64
		insertErr := tx.QueryRowContext(ctx, insertOccurrenceSQL,
			tenantID, run.SourceUID, revisionID, run.OccurrenceKey,
			string(run.Trigger), actor, string(jobrun.Pending), maxAttempts,
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
// The statement writes when the phase or the attempt counter would change, and
// only then. Skipping a true no-op keeps a resync from rewriting the row, but
// the counter is still corrected when it lags the attempts table -- that lag is
// what makes the next reconcile start an attempt for a Job already running.
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
RETURNING r.phase, prior.prior_phase, prior.prior_attempt, prior.run_actor`

const selectRunProgressSQL = `SELECT phase, attempt FROM job_run_control_plane WHERE id=$1 AND tenant_id=$2`

// UpdateRunPhase advances a run to a new phase and, in the same write, the
// attempt counter the next reconcile reads. It reports whether this write
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
			return nil
		}
		if jobrun.Terminal(jobrun.Phase(current)) {
			return jobrun.ErrRunTerminal
		}
		return fmt.Errorf("run %d phase write matched no row while in phase %s attempt %d",
			runID, current, currentAttempt)
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
SELECT id, revision_id, occurrence_key, phase, scheduled_for
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
			if scanErr := rows.Scan(&run.ID, &run.RevisionID, &run.OccurrenceKey, &phase, &scheduledFor); scanErr != nil {
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
SELECT id, occurrence_key, phase FROM (
  SELECT id, occurrence_key, phase,
         row_number() OVER (PARTITION BY phase ORDER BY created_at DESC, id DESC) AS rank,
         CASE WHEN phase = $4 THEN $5::int ELSE $6::int END AS keep
  FROM job_run_control_plane
  WHERE tenant_id=$1 AND source_uid=$2 AND phase = ANY($3) AND workflow_run_id IS NULL
) ranked
WHERE rank > keep
ORDER BY id`

// retentionPhases is the set retention may delete: the terminal phases plus
// CancelUnknown, a stop the platform issued and never observed, which retention
// resolves after the same window as the failures.
func retentionPhases() []string {
	return append(terminalPhases(), string(jobrun.CancelUnknown))
}

// PrunableRuns lists runs beyond the retained history for a definition. Rows are
// ranked per outcome, so a burst of failures cannot evict the success history
// and vice versa. Only phases in retentionPhases are eligible, and a run that
// started executing between the scan and the delete is left alone by DeleteRun.
//
// retentionPhases includes CancelUnknown, which jobrun.Terminal does not call
// terminal: a run whose stop was never observed may still be live, so pruning it
// can orphan its Kubernetes Job. That is the retention policy handed down here,
// not a decision this statement makes.
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
			if scanErr := rows.Scan(&run.ID, &run.OccurrenceKey, &phase); scanErr != nil {
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
		var (
			occurrenceKey string
			phase         string
			actor         string
		)
		scanErr := tx.QueryRowContext(ctx, deleteRunSQL, runID, tenantID, terminalPhases()).
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
  SET kubernetes_job_uid = COALESCE(NULLIF(EXCLUDED.kubernetes_job_uid,''),
                                     job_run_attempts_control_plane.kubernetes_job_uid),
      observed_resource_version = COALESCE(NULLIF(EXCLUDED.observed_resource_version,''),
                                     job_run_attempts_control_plane.observed_resource_version)
  WHERE job_run_attempts_control_plane.kubernetes_job_name = EXCLUDED.kubernetes_job_name
RETURNING id`

const advanceRunForAttemptSQL = `
UPDATE job_run_control_plane
SET phase=$1, attempt=$2, updated_at=$3
WHERE id=$4 AND tenant_id=$5 AND NOT (phase = ANY($6))
RETURNING actor`

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
	if attempt.Number < 1 || attempt.KubernetesJobName == "" {
		return fmt.Errorf("attempt identity is required")
	}
	return r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
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
		if err := tx.QueryRowContext(ctx, advanceRunForAttemptSQL,
			string(runPhase), attempt.Number, r.now().UTC(), runID, tenantID, terminalPhases(),
		).Scan(&runActor); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// The run reached a terminal phase between the caller's read and
				// this write. Rolling back takes the attempt row with it, so the
				// ledger never claims an attempt for a finished run.
				return jobrun.ErrRunTerminal
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
// $6 is jobrun.Terminal of the incoming phase rather than a phase list: the
// domain predicate is the authority on what finished means, and passing it
// removes a second copy of the terminal set from this statement.
const updateAttemptPhaseSQL = `
WITH prior AS (
  SELECT id, run_id, attempt_number, phase AS prior_phase
  FROM job_run_attempts_control_plane
  WHERE kubernetes_job_name=$1 AND tenant_id=$2
  FOR UPDATE
)
UPDATE job_run_attempts_control_plane a
SET phase=$3,
    kubernetes_job_uid = COALESCE(NULLIF($4,''), a.kubernetes_job_uid),
    observed_resource_version = COALESCE(NULLIF($5,''), a.observed_resource_version),
    completed_at = CASE WHEN $6 AND a.completed_at IS NULL
                        THEN $7::timestamptz ELSE a.completed_at END
FROM prior
WHERE a.kubernetes_job_name=$1 AND a.tenant_id=$2
RETURNING a.id, a.run_id, a.attempt_number, prior.prior_phase, a.phase,
          (SELECT r.actor FROM job_run_control_plane r WHERE r.id = prior.run_id)`

// UpdateAttemptPhase writes an observed Kubernetes Job phase back onto the
// attempt and reports which run and attempt it belongs to. Terminal phases stamp
// completed_at exactly once. ErrNoAttempt is returned when no attempt owns that
// Job name, so a Job left over from a previous installation is not mistaken for
// this attempt's.
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
			attemptID  int64
			priorPhase string
			newPhase   string
			runActor   string
		)
		scanErr := tx.QueryRowContext(ctx, updateAttemptPhaseSQL,
			jobName, tenantID, phase, jobUID, resourceVersion,
			jobrun.Terminal(jobrun.Phase(phase)), r.now().UTC(),
		).Scan(&attemptID, &runID, &attemptNumber, &priorPhase, &newPhase, &runActor)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return ErrNoAttempt
		}
		if scanErr != nil {
			return scanErr
		}
		if priorPhase == newPhase {
			return nil
		}
		return insertControlPlaneAudit(ctx, tx, tenantID, runActor, auditEventAttemptStatusChanged,
			auditResourceAttempt, strconv.FormatInt(attemptID, 10), map[string]any{
				"run_id":         runID,
				"attempt_number": attemptNumber,
				"from_phase":     priorPhase,
				"to_phase":       newPhase,
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

const terminalOutcomeSQL = `
SELECT r.source_uid, r.occurrence_key, r.scheduled_for, r.phase,
       a.started_at, a.completed_at
FROM job_run_control_plane r
LEFT JOIN LATERAL (
  SELECT started_at, completed_at
  FROM job_run_attempts_control_plane a
  WHERE a.run_id = r.id
  ORDER BY a.attempt_number DESC
  LIMIT 1
) a ON true
WHERE r.id = $1 AND r.tenant_id = $2`

// TerminalOutcome reads everything a terminal run's bookkeeping needs in one
// query: the definition it ran for, the occurrence it was for, when it was
// scheduled, and the wall-clock span of its newest attempt. The read model and
// SLI derivation are written outside the phase transition's transaction, so
// this read is their consistent view of the fact the transition committed.
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
			Scan(&out.SourceUID, &out.OccurrenceKey, &scheduledFor, &phase, &startedAt, &completedAt)
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

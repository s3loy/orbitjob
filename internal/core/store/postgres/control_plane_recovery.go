package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"

	"github.com/lib/pq"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

// DeactivateSource preserves historical revisions while removing admission.
func (r *ControlPlaneRepository) DeactivateSource(ctx context.Context, tenantID, mode, uid string) error {
	if uid == "" {
		return fmt.Errorf("source UID is required")
	}
	// First projection may hold this lock without a committed revision row yet.
	// A known identity must participate even when candidate discovery sees none.
	return r.deactivateSources(ctx, tenantID, mode, []string{uid}, `source_uid=$3`, uid)
}
func (r *ControlPlaneRepository) DeactivateSourceByName(ctx context.Context, tenantID, mode, namespace, name string) error {
	return r.deactivateSources(ctx, tenantID, mode, nil, `source_namespace=$3 AND source_name=$4`, namespace, name)
}

// A completed inventory is a source of candidates, not a cross-API fence.
func (r *ControlPlaneRepository) DeactivateSourcesMissing(ctx context.Context, tenantID, mode string, liveUIDs []string) error {
	return r.deactivateSources(ctx, tenantID, mode, nil, `NOT (source_uid=ANY($3))`, pq.Array(nonNilStrings(liveUIDs)))
}
func (r *ControlPlaneRepository) deactivateSources(ctx context.Context, tenant, mode string, uids []string, predicate string, args ...any) error {
	if mode == "" {
		return fmt.Errorf("source mode is required")
	}
	return withAdmissionTx(ctx, r.db, tenant, func(ctx context.Context, tx *sql.Tx) error {
		binds := append([]any{tenant, mode}, args...)
		where := `tenant_id=$1 AND source_mode=$2 AND ` + predicate
		if uids == nil {
			// An inactive historical UID can be activating in another transaction.
			// Enumerate all known matching identities, then recheck after the lock.
			rows, err := tx.QueryContext(ctx, `SELECT DISTINCT source_uid FROM job_definition_revisions WHERE `+where, binds...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var uid string
				if err = rows.Scan(&uid); err != nil {
					_ = rows.Close()
					return err
				}
				uids = append(uids, uid)
			}
			scanErr := rows.Err()
			closeErr := rows.Close()
			if scanErr != nil {
				return scanErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
		sort.Strings(uids)
		// No revision row is locked before every source advisory lock is acquired.
		for _, uid := range uids {
			if err := lockSource(ctx, tx, tenant, mode, uid); err != nil {
				return err
			}
		}
		for _, uid := range uids {
			if err := markSourceDeleted(ctx, tx, tenant, mode, uid); err != nil {
				return err
			}
			updated, err := tx.QueryContext(ctx, `UPDATE job_definition_revisions SET is_active=false WHERE `+where+fmt.Sprintf(` AND is_active AND source_uid=$%d RETURNING id`, len(binds)+1), append(append([]any{}, binds...), uid)...)
			if err != nil {
				return err
			}
			var ids []int64
			for updated.Next() {
				var id int64
				if err = updated.Scan(&id); err != nil {
					_ = updated.Close()
					return err
				}
				ids = append(ids, id)
			}
			scanErr := updated.Err()
			closeErr := updated.Close()
			if scanErr != nil {
				return scanErr
			}
			if closeErr != nil {
				return closeErr
			}
			for _, id := range ids {
				if err = insertControlPlaneAudit(ctx, tx, tenant, "operator", "revision.deactivated", auditResourceRevision, strconv.FormatInt(id, 10), map[string]any{"is_active": false}); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// TerminalOutcomesPending discovers committed facts whose derived models have
// not all been acknowledged. Projection writes themselves must be idempotent.
func (r *ControlPlaneRepository) TerminalOutcomesPending(ctx context.Context, tenantID string, limit int) ([]controlplane.TerminalOutcome, error) {
	return r.TerminalOutcomesPendingAfter(ctx, tenantID, 0, limit)
}

// TerminalOutcomesPendingAfter is a pure keyset read. The consumer owns its
// cursor and may wrap to zero without mutating or acknowledging failed facts.
func (r *ControlPlaneRepository) TerminalOutcomesPendingAfter(ctx context.Context, tenantID string, afterID int64, limit int) ([]controlplane.TerminalOutcome, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	var out []controlplane.TerminalOutcome
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, source_uid, occurrence_key, scheduled_for, created_at, phase, started_at, completed_at FROM (`+terminalOutcomeSelectSQL+`) r `+`
WHERE r.tenant_id=$1 AND r.phase=ANY($2) AND r.outcome_recorded_at IS NULL AND r.id>$3
ORDER BY r.id LIMIT $4`, tenantID, terminalPhases(), afterID, limit)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var record controlplane.TerminalOutcome
			var phase string
			var scheduled, started, completed sql.NullTime
			if err := rows.Scan(&record.RunID, &record.SourceUID, &record.OccurrenceKey, &scheduled, &record.CreatedAt, &phase, &started, &completed); err != nil {
				return err
			}
			record.Phase = jobrun.Phase(phase)
			if scheduled.Valid {
				record.ScheduledFor = scheduled.Time
			}
			if started.Valid {
				record.StartedAt = started.Time
			}
			if completed.Valid {
				record.CompletedAt = completed.Time
			}
			out = append(out, record)
		}
		return rows.Err()
	})
	return out, err
}

func (r *ControlPlaneRepository) MarkOutcomeRecorded(ctx context.Context, tenantID string, runID int64) error {
	return r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE job_run_control_plane
SET outcome_recorded_at=COALESCE(outcome_recorded_at,now())
WHERE tenant_id=$1 AND id=$2 AND phase=ANY($3)`, tenantID, runID, terminalPhases())
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("terminal run %d is unavailable for outcome acknowledgement", runID)
		}
		return nil
	})
}

// RetentionRevisions also includes inactive definitions: deleting a source must
// stop scheduling without making its already committed history unreachable.
func (r *ControlPlaneRepository) RetentionRevisions(ctx context.Context, tenantID string) ([]revision.Revision, error) {
	return r.retentionRevisions(ctx, tenantID, revisionSourceModes())
}

func (r *ControlPlaneRepository) RetentionWorkflowRevisions(ctx context.Context, tenantID string) ([]revision.Revision, error) {
	return r.retentionRevisions(ctx, tenantID, []string{workflow.SourceModeWorkflow})
}

func (r *ControlPlaneRepository) retentionRevisions(ctx context.Context, tenantID string, modes []string) ([]revision.Revision, error) {
	var out []revision.Revision
	err := r.WithTenantTransaction(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT ON (source_mode,source_uid) `+revisionColumns+`
FROM job_definition_revisions WHERE tenant_id=$1 AND source_mode=ANY($2)
ORDER BY source_mode,source_uid,generation DESC`, tenantID, modes)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		out, err = scanRevisions(rows)
		return err
	})
	return out, err
}

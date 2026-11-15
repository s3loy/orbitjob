package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"orbitjob/internal/core/app/slimaintenance"
	"orbitjob/internal/core/domain/slo"
)

const sliRetirementItemTimeout = time.Second

type sliRetirementCandidate struct {
	runID  int64
	sliID  int64
	source string
}

// PruneRetiredEvents retains its bounded one-page entry point. Periodic callers
// use PruneRetiredEventsPage to rotate past protected records across ticks.
func (r *SLIRepository) PruneRetiredEvents(ctx context.Context, tenant string, before time.Time, limit int) (int, error) {
	page, err := r.PruneRetiredEventsPage(ctx, tenant, before, slimaintenance.Cursor{}, limit)
	return page.Counts.RemovedPlans + page.Counts.RemovedLegacyEvents, err
}

// Candidate discovery follows the existing tenant-prefixed primary keys and
// inspects at most limit rows of each kind. Filtering on age before LIMIT would
// permit an unbounded scan of young or protected records.
func (r *SLIRepository) PruneRetiredEventsPage(ctx context.Context, tenant string, before time.Time, cursor slimaintenance.Cursor, limit int) (slimaintenance.Page, error) {
	page := slimaintenance.Page{Next: cursor, Counts: slimaintenance.Counts{Retained: make(map[string]int)}}
	if limit < 1 || limit > slimaintenance.DefaultBatchSize {
		limit = slimaintenance.DefaultBatchSize
	}
	var plans, events []sliRetirementCandidate
	err := withAdmissionTx(ctx, r.db, tenant, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		plans, err = sliRetirementCandidates(ctx, tx, tenant, cursor, limit, true)
		if err != nil {
			return err
		}
		if len(plans) == 0 && cursor.PlanRunID != 0 {
			page.Next.PlanRunID = 0
			plans, err = sliRetirementCandidates(ctx, tx, tenant, slimaintenance.Cursor{}, limit, true)
			if err != nil {
				return err
			}
		}
		events, err = sliRetirementCandidates(ctx, tx, tenant, cursor, limit, false)
		if err != nil {
			return err
		}
		if len(events) == 0 && (cursor.EventRunID != 0 || cursor.EventSLIID != 0) {
			page.Next.EventRunID, page.Next.EventSLIID = 0, 0
			events, err = sliRetirementCandidates(ctx, tx, tenant, slimaintenance.Cursor{}, limit, false)
		}
		return err
	})
	if err != nil {
		return page, fmt.Errorf("scan SLI retirement page: %w", err)
	}
	var failures error
	for _, c := range plans {
		if err := ctx.Err(); err != nil {
			return page, errors.Join(failures, err)
		}
		page.Next.PlanRunID = c.runID
		page.Counts.ScannedPlans++
		itemCtx, cancel := context.WithTimeout(ctx, sliRetirementItemTimeout)
		count, reason, err := r.retireSLIPlan(itemCtx, tenant, before, c)
		cancel()
		if err != nil {
			failures = errors.Join(failures, fmt.Errorf("retire SLI plan %d: %w", c.runID, err))
			continue
		}
		if reason != "" {
			page.Counts.Retained[reason]++
			continue
		}
		if count >= 0 {
			page.Counts.RemovedPlans++
			page.Counts.RemovedEvents += count
		}
	}
	for _, c := range events {
		if err := ctx.Err(); err != nil {
			return page, errors.Join(failures, err)
		}
		page.Next.EventRunID, page.Next.EventSLIID = c.runID, c.sliID
		page.Counts.ScannedEvents++
		itemCtx, cancel := context.WithTimeout(ctx, sliRetirementItemTimeout)
		deleted, reason, err := r.retireLegacySLIEvent(itemCtx, tenant, before, c)
		cancel()
		if err != nil {
			failures = errors.Join(failures, fmt.Errorf("retire legacy SLI event %d/%d: %w", c.runID, c.sliID, err))
			continue
		}
		if reason != "" {
			page.Counts.Retained[reason]++
		}
		if deleted {
			page.Counts.RemovedEvents++
			page.Counts.RemovedLegacyEvents++
		}
	}
	return page, failures
}

func sliRetirementCandidates(ctx context.Context, tx *sql.Tx, tenant string, cursor slimaintenance.Cursor, limit int, plans bool) ([]sliRetirementCandidate, error) {
	query := `SELECT run_id,source_uid FROM sli_event_plans WHERE tenant_id=$1 AND run_id>$2 ORDER BY run_id LIMIT $3`
	args := []any{tenant, cursor.PlanRunID, limit}
	if !plans {
		query = `SELECT run_id,sli_id FROM sli_snapshot_events WHERE tenant_id=$1 AND (run_id,sli_id)>($2,$3) ORDER BY run_id,sli_id LIMIT $4`
		args = []any{tenant, cursor.EventRunID, cursor.EventSLIID, limit}
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []sliRetirementCandidate
	for rows.Next() {
		var c sliRetirementCandidate
		if plans {
			err = rows.Scan(&c.runID, &c.source)
		} else {
			err = rows.Scan(&c.runID, &c.sliID)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *SLIRepository) retireSLIPlan(ctx context.Context, tenant string, before time.Time, c sliRetirementCandidate) (int, string, error) {
	removed, reason := -1, ""
	err := withAdmissionTx(ctx, r.db, tenant, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT orbitjob_lock_sli_source($1,$2)`, tenant, c.source); err != nil {
			return err
		}
		if err := lockSLIEventRun(ctx, tx, tenant, c.runID); err != nil {
			return err
		}
		var hasRun, young, incomplete, hasRecentMissing bool
		err := tx.QueryRowContext(ctx, `WITH boundary AS (
 SELECT LEAST($3::timestamptz,clock_timestamp()-interval '30 days') AS replay,
 LEAST($3::timestamptz,clock_timestamp())-($4::bigint*interval '1 second') AS evidence)
 SELECT EXISTS(SELECT 1 FROM job_run_control_plane r WHERE r.tenant_id=p.tenant_id AND r.id=p.run_id),
 p.created_at>=b.replay OR EXISTS(SELECT 1 FROM sli_snapshot_events e WHERE e.tenant_id=p.tenant_id AND e.run_id=p.run_id AND e.created_at>=b.replay),
 EXISTS(SELECT 1 FROM sli_event_plan_members m WHERE m.tenant_id=p.tenant_id AND m.run_id=p.run_id
 AND NOT EXISTS(SELECT 1 FROM sli_snapshot_events e WHERE e.tenant_id=m.tenant_id AND e.run_id=m.run_id AND e.sli_id=m.sli_id
 AND e.configuration_version=m.configuration_version AND e.semantics=p.semantics
 AND (p.semantics='legacy_processing_v1' OR (e.event_time=p.event_time AND e.time_quality=p.time_quality))
 AND (e.plan_run_id=p.run_id OR (p.semantics='legacy_processing_v1' AND e.plan_run_id IS NULL)))),
 EXISTS(SELECT 1 FROM sli_snapshot_events e WHERE e.tenant_id=p.tenant_id AND e.run_id=p.run_id
 AND e.evaluation_state='unavailable' AND (e.event_time IS NULL OR e.event_time>=b.evidence))
 FROM sli_event_plans p CROSS JOIN boundary b WHERE p.tenant_id=$1 AND p.run_id=$2 AND p.source_uid=$5`, tenant, c.runID, before, int64(slo.MaxWindowDuration/time.Second), c.source).Scan(&hasRun, &young, &incomplete, &hasRecentMissing)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		switch {
		case hasRun:
			reason = "run_present"
		case incomplete:
			reason = "incomplete_plan"
		case young:
			reason = "replay_horizon"
		case hasRecentMissing:
			reason = "queryable_missing_evidence"
		}
		if reason != "" {
			return nil
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM sli_snapshot_events WHERE tenant_id=$1 AND run_id=$2`, tenant, c.runID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		result, err = tx.ExecContext(ctx, `DELETE FROM sli_event_plans WHERE tenant_id=$1 AND run_id=$2`, tenant, c.runID)
		if err != nil {
			return err
		}
		plans, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if plans != 1 {
			return fmt.Errorf("SLI plan changed during retirement")
		}
		removed = int(count)
		return nil
	})
	if err != nil {
		return -1, "", err
	}
	return removed, reason, nil
}

func (r *SLIRepository) retireLegacySLIEvent(ctx context.Context, tenant string, before time.Time, c sliRetirementCandidate) (bool, string, error) {
	deleted, reason := false, ""
	err := withAdmissionTx(ctx, r.db, tenant, func(ctx context.Context, tx *sql.Tx) error {
		if err := lockSLIEventRun(ctx, tx, tenant, c.runID); err != nil {
			return err
		}
		var hasRun, hasPlan, compatible, young, hasRecentMissing bool
		err := tx.QueryRowContext(ctx, `WITH boundary AS (
 SELECT LEAST($4::timestamptz,clock_timestamp()-interval '30 days') AS replay,
 LEAST($4::timestamptz,clock_timestamp())-($5::bigint*interval '1 second') AS evidence)
 SELECT EXISTS(SELECT 1 FROM job_run_control_plane r WHERE r.tenant_id=e.tenant_id AND r.id=e.run_id),
 EXISTS(SELECT 1 FROM sli_event_plans p WHERE p.tenant_id=e.tenant_id AND p.run_id=e.run_id),
 e.plan_run_id IS NULL AND e.semantics='legacy_processing_v1' AND e.configuration_version=1,
 e.created_at>=b.replay,
 e.evaluation_state='unavailable' AND (e.event_time IS NULL OR e.event_time>=b.evidence)
 FROM sli_snapshot_events e CROSS JOIN boundary b WHERE e.tenant_id=$1 AND e.run_id=$2 AND e.sli_id=$3`, tenant, c.runID, c.sliID, before, int64(slo.MaxWindowDuration/time.Second)).Scan(&hasRun, &hasPlan, &compatible, &young, &hasRecentMissing)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		switch {
		case hasPlan:
			reason = "planned_event"
		case hasRun:
			reason = "run_present"
		case !compatible:
			reason = "unsupported_legacy_event"
		case young:
			reason = "replay_horizon"
		case hasRecentMissing:
			reason = "queryable_missing_evidence"
		}
		if reason != "" {
			return nil
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM sli_snapshot_events WHERE tenant_id=$1 AND run_id=$2 AND sli_id=$3`, tenant, c.runID, c.sliID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		deleted = count == 1
		return nil
	})
	if err != nil {
		return false, "", err
	}
	return deleted, reason, nil
}

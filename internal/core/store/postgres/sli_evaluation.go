package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"orbitjob/internal/core/domain/sli"
)

// RecordEvaluation atomically records a plan-authorized fact, its optional
// counter delta and freshness. Missing timing is retained without a bad sample.
func (r *SLISnapshotRepository) RecordEvaluation(ctx context.Context, tenant string, e sli.Evaluation) (bool, error) {
	if e.RunID < 1 || e.SLIID < 1 || e.ConfigurationVersion < 1 || e.EventTime.IsZero() || !e.WindowStart.Equal(e.EventTime.UTC().Truncate(defaultSnapshotWindow)) {
		return false, fmt.Errorf("invalid SLI event identity or window")
	}
	if (e.Good == nil && (e.MissingReason == "" || len(e.MissingReason) > 128)) || (e.Good != nil && e.MissingReason != "") {
		return false, fmt.Errorf("invalid SLI missing-fact state")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenant); err != nil {
		return false, err
	}
	var source string
	if err = tx.QueryRowContext(ctx, `SELECT source_uid FROM sli_event_plans WHERE tenant_id=$1 AND run_id=$2`, tenant, e.RunID).Scan(&source); err != nil {
		return false, fmt.Errorf("read SLI event plan: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT orbitjob_lock_sli_source($1,$2)`, tenant, source); err != nil {
		return false, err
	}
	if err = lockSLIEventRun(ctx, tx, tenant, e.RunID); err != nil {
		return false, err
	}
	state := "evaluated"
	var reason any
	if e.Good == nil {
		state = "unavailable"
		reason = e.MissingReason
	}
	var runID int64
	err = tx.QueryRowContext(ctx, `INSERT INTO sli_snapshot_events(tenant_id,run_id,sli_id,window_start,is_good,configuration_version,semantics,event_time,time_quality,evaluation_state,missing_reason,plan_run_id)
 SELECT p.tenant_id,p.run_id,m.sli_id,$5,$6,m.configuration_version,p.semantics,p.event_time,p.time_quality,$7,$8,p.run_id
 FROM sli_event_plans p JOIN sli_event_plan_members m ON m.tenant_id=p.tenant_id AND m.run_id=p.run_id
 WHERE p.tenant_id=$1 AND p.run_id=$2 AND m.sli_id=$3 AND m.configuration_version=$4
 AND p.event_time=$9 AND p.semantics=$10 AND p.time_quality=$11
 ON CONFLICT(tenant_id,run_id,sli_id) DO NOTHING RETURNING run_id`, tenant, e.RunID, e.SLIID, e.ConfigurationVersion, e.WindowStart, e.Good, state, reason, e.EventTime, e.Semantics, e.TimeQuality).Scan(&runID)
	if err == sql.ErrNoRows {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sli_snapshot_events WHERE tenant_id=$1 AND run_id=$2 AND sli_id=$3)`, tenant, e.RunID, e.SLIID).Scan(&exists); err != nil {
			return false, err
		}
		if !exists {
			return false, fmt.Errorf("SLI plan does not authorize this evaluation")
		}
		return false, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	if e.Good != nil {
		good := int64(0)
		if *e.Good {
			good = 1
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO sli_snapshots(tenant_id,sli_id,window_start,window_end,good_events_count,total_events_count,sli_value,statistics_semantics,last_event_at,last_sample_at,last_observed_at)
 VALUES($1,$2,$3,$4,$5,1,$5::bigint::decimal,$6,$7,$7,clock_timestamp())
 ON CONFLICT(tenant_id,sli_id,window_start) DO UPDATE SET
 good_events_count=sli_snapshots.good_events_count+EXCLUDED.good_events_count,total_events_count=sli_snapshots.total_events_count+1,
 sli_value=(sli_snapshots.good_events_count+EXCLUDED.good_events_count)::decimal/(sli_snapshots.total_events_count+1),
 statistics_semantics=CASE WHEN sli_snapshots.statistics_semantics=EXCLUDED.statistics_semantics THEN EXCLUDED.statistics_semantics ELSE 'mixed' END,
 last_event_at=GREATEST(sli_snapshots.last_event_at,EXCLUDED.last_event_at),last_sample_at=GREATEST(sli_snapshots.last_sample_at,EXCLUDED.last_sample_at),
 last_observed_at=EXCLUDED.last_observed_at,updated_at=clock_timestamp()`, tenant, e.SLIID, e.WindowStart, e.WindowStart.Add(defaultSnapshotWindow), good, e.Semantics, e.EventTime)
		if err != nil {
			return false, err
		}
	}
	var sampleAt any
	if e.Good != nil {
		sampleAt = e.EventTime
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sli_observation_state(tenant_id,sli_id,statistics_semantics,last_event_at,last_sample_at,last_observed_at)
 VALUES($1,$2,$3,$4,$5,clock_timestamp()) ON CONFLICT(tenant_id,sli_id) DO UPDATE SET
 statistics_semantics=CASE WHEN sli_observation_state.last_observed_at IS NULL OR sli_observation_state.statistics_semantics=EXCLUDED.statistics_semantics THEN EXCLUDED.statistics_semantics ELSE 'mixed' END,
 last_event_at=GREATEST(sli_observation_state.last_event_at,EXCLUDED.last_event_at),last_sample_at=GREATEST(sli_observation_state.last_sample_at,EXCLUDED.last_sample_at),last_observed_at=EXCLUDED.last_observed_at`, tenant, e.SLIID, e.Semantics, e.EventTime, sampleAt)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

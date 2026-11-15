package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"orbitjob/internal/core/app/sloevaluate"
)

const defaultSnapshotWindow = 5 * time.Minute

// SLISnapshotRepository manages pre-aggregated SLI snapshots.
type SLISnapshotRepository struct {
	db *sql.DB
}

// NewSLISnapshotRepository creates a new snapshot repository.
func NewSLISnapshotRepository(db *sql.DB) *SLISnapshotRepository {
	return &SLISnapshotRepository{db: db}
}

// IncrementSnapshot atomically increments the snapshot for a given window.
// Uses UPSERT (INSERT ... ON CONFLICT DO UPDATE) for atomicity.
func (r *SLISnapshotRepository) IncrementSnapshot(ctx context.Context, tenantID string, sliID int64, windowStart time.Time, isGood bool) error {
	windowEnd := windowStart.Add(defaultSnapshotWindow)
	goodDelta := int64(0)
	if isGood {
		goodDelta = 1
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin increment tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	// $5 is cast explicitly at both uses: the good_events_count column deduces
	// bigint while the sli_value expression would deduce decimal, and one
	// parameter inferred as two types is refused (42P08) at execute time.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sli_snapshots (tenant_id, sli_id, window_start, window_end, good_events_count, total_events_count, sli_value)
		VALUES ($1, $2, $3, $4, $5::bigint, 1, $5::bigint::decimal / 1)
		ON CONFLICT (tenant_id, sli_id, window_start)
		DO UPDATE SET
			good_events_count = sli_snapshots.good_events_count + EXCLUDED.good_events_count,
			total_events_count = sli_snapshots.total_events_count + 1,
			sli_value = (sli_snapshots.good_events_count + EXCLUDED.good_events_count)::DECIMAL
			            / (sli_snapshots.total_events_count + 1),
			updated_at = now()
	`, tenantID, sliID, windowStart, windowEnd, goodDelta)
	if err != nil {
		return fmt.Errorf("upsert sli snapshot: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit increment snapshot: %w", err)
	}

	return nil
}

// AggregateWindow aggregates all snapshots within a time window for an SLI.
func (r *SLISnapshotRepository) AggregateWindow(ctx context.Context, tenantID string, sliID int64, start, end time.Time) (sloevaluate.WindowAggregate, error) {
	var agg sloevaluate.WindowAggregate
	var event, sample sql.NullTime
	var sourceAvailable bool
	var sourceUID string

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return agg, fmt.Errorf("begin aggregate tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return agg, fmt.Errorf("set tenant context: %w", err)
	}

	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(good_events_count), 0), COALESCE(SUM(total_events_count), 0),
        max(o.last_event_at),max(o.last_sample_at),
        CASE WHEN count(DISTINCT p.statistics_semantics)>1 OR bool_or(p.statistics_semantics='mixed') THEN 'mixed'
        ELSE COALESCE(min(p.statistics_semantics),max(o.statistics_semantics),'event_time_v2') END,
        count(s.id)>0 AND COALESCE(bool_and(s.deleted_at IS NULL),false),
        COALESCE((SELECT missing_reason FROM sli_snapshot_events e WHERE e.tenant_id=$1 AND e.sli_id=$2
        AND e.evaluation_state='unavailable' AND e.event_time>=$3 AND e.event_time<$4 ORDER BY e.event_time DESC LIMIT 1),''),
        COALESCE(min(s.source_config->>'source_uid'),'')
		FROM slis s LEFT JOIN sli_snapshots p ON p.tenant_id=s.tenant_id AND p.sli_id=s.id AND p.window_start >= $3 AND p.window_start < $4
        LEFT JOIN sli_observation_state o ON o.tenant_id=s.tenant_id AND o.sli_id=s.id
		WHERE s.tenant_id = $1 AND s.id = $2
	`, tenantID, sliID, start, end).Scan(&agg.GoodEventsCount, &agg.TotalEventsCount, &event, &sample, &agg.StatisticsSemantics, &sourceAvailable, &agg.MissingReason, &sourceUID)
	if err != nil {
		return agg, fmt.Errorf("aggregate sli snapshots: %w", err)
	}

	if agg.TotalEventsCount > 0 {
		agg.SLIValue = float64(agg.GoodEventsCount) / float64(agg.TotalEventsCount)
	}
	if event.Valid {
		agg.LastEventAt = &event.Time
	}
	if sample.Valid {
		agg.LastSampleAt = &sample.Time
	}
	agg.SourceAvailable = &sourceAvailable
	if sourceAvailable {
		active, deadline, sourceErr := sliSourceFreshness(ctx, tx, tenantID, sourceUID, agg.LastSampleAt)
		if sourceErr != nil {
			return agg, sourceErr
		}
		agg.SourceAvailable = &active
		agg.FreshUntil = deadline
	}

	if err = tx.Commit(); err != nil {
		return agg, fmt.Errorf("commit aggregate snapshot: %w", err)
	}

	return agg, nil
}

// IncrementSnapshotForRun records the run/SLI fact and its aggregate delta in
// one transaction. Replays remain harmless even after the run ledger is pruned.
func (r *SLISnapshotRepository) IncrementSnapshotForRun(ctx context.Context, tenantID string, runID, sliID int64, windowStart time.Time, isGood bool) (bool, error) {
	if runID < 1 || sliID < 1 {
		return false, fmt.Errorf("run and sli identity are required")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, fmt.Errorf("begin snapshot event: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return false, fmt.Errorf("set tenant context: %w", err)
	}
	if err = lockSLIEventRun(ctx, tx, tenantID, runID); err != nil {
		return false, fmt.Errorf("lock SLI event run: %w", err)
	}
	var count int64
	err = tx.QueryRowContext(ctx, `WITH event AS (
 INSERT INTO sli_snapshot_events(tenant_id,run_id,sli_id,window_start,is_good)
 SELECT $1,$2,$3,$4,$5 WHERE EXISTS(SELECT 1 FROM job_run_control_plane WHERE tenant_id=$1 AND id=$2 AND phase IN('Succeeded','Failed'))
 ON CONFLICT (tenant_id,run_id,sli_id) DO NOTHING RETURNING sli_id
 )
 INSERT INTO sli_snapshots(tenant_id,sli_id,window_start,window_end,good_events_count,total_events_count,sli_value)
 SELECT $1,sli_id,$4,$6,CASE WHEN $5 THEN 1 ELSE 0 END,1,CASE WHEN $5 THEN 1 ELSE 0 END FROM event
 ON CONFLICT (tenant_id,sli_id,window_start) DO UPDATE SET
 good_events_count=sli_snapshots.good_events_count+EXCLUDED.good_events_count,
 total_events_count=sli_snapshots.total_events_count+1,
 sli_value=(sli_snapshots.good_events_count+EXCLUDED.good_events_count)::decimal/(sli_snapshots.total_events_count+1),
 updated_at=now() RETURNING total_events_count`, tenantID, runID, sliID, windowStart.UTC(), isGood, windowStart.UTC().Add(defaultSnapshotWindow)).Scan(&count)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("increment snapshot event: %w", err)
	}
	created := err == nil
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit snapshot event: %w", err)
	}
	return created, nil
}

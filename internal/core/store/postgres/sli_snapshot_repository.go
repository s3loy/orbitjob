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

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sli_snapshots (tenant_id, sli_id, window_start, window_end, good_events_count, total_events_count, sli_value)
		VALUES ($1, $2, $3, $4, $5, 1, $5::decimal / 1)
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

	return nil
}

// AggregateWindow aggregates all snapshots within a time window for an SLI.
func (r *SLISnapshotRepository) AggregateWindow(ctx context.Context, tenantID string, sliID int64, start, end time.Time) (sloevaluate.WindowAggregate, error) {
	var agg sloevaluate.WindowAggregate

	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(good_events_count), 0), COALESCE(SUM(total_events_count), 0)
		FROM sli_snapshots
		WHERE tenant_id = $1 AND sli_id = $2 AND window_start >= $3 AND window_start < $4
	`, tenantID, sliID, start, end).Scan(&agg.GoodEventsCount, &agg.TotalEventsCount)
	if err != nil {
		return agg, fmt.Errorf("aggregate sli snapshots: %w", err)
	}

	if agg.TotalEventsCount > 0 {
		agg.SLIValue = float64(agg.GoodEventsCount) / float64(agg.TotalEventsCount)
	}

	return agg, nil
}

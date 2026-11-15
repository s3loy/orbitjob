BEGIN;

-- Unevaluated measurements retain an explicit cause. Historical budget rows
-- keep their original values; an empty reason makes no new freshness claim.
ALTER TABLE budgets ADD COLUMN evaluation_reason VARCHAR(32) NOT NULL DEFAULT ''
 CHECK(evaluation_reason IN('','no_samples','source_inactive','missing_timing','stale','dependency_unavailable'));

-- Late and missing facts are queried by their source event time, independently
-- of projection time and the counter window. This index contains no output.
CREATE INDEX idx_sli_snapshot_events_time ON sli_snapshot_events(tenant_id,sli_id,event_time DESC)
 WHERE event_time IS NOT NULL;

COMMIT;

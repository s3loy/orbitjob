BEGIN;

-- ============================================================
-- Migration 0002: checks execute through the Kubernetes control plane
-- ============================================================
--
-- One product decision, three schema effects (design:
-- docs/superpowers/plans/checks-to-k8s-jobs-design.md, sections 5.2b, 5.3,
-- and the rulings that hard-cut the SLI source over).
--
-- 1. job_run_control_plane.scheduled_for
--    The occurrence a scheduled run was for, in civil time. The occurrence
--    key hashes this instant, so today a schedule-adherence question ("did
--    the 03:00 job run") can only be answered by re-deriving keys. Written
--    by the scheduler at row creation from the occurrence it is firing;
--    nullable because manual runs have no scheduled instant, and no backfill
--    is possible (the instant was only ever a CR annotation the ledger never
--    ingested).
--
-- 2. slis.source_type cutover to 'job_run'
--    SLI events now derive from the run ledger. An SLI names its source
--    definition by source_uid in source_config
--    ({"source_uid": "check-42"} for a check, the ScheduledJob CR UID for a
--    declared job); the old 'check_run' source read check_runs, which stops
--    being a work queue and becomes a derived read model with no execution
--    of its own. Existing check_run rows are rewritten to the new shape so
--    the linkage survives; 'check-<check_id>' is the same mapping the
--    check-firing loop uses for its revision source_uid.
--
-- 3. sli_snapshots truncated
--    Every snapshot row was written by the dead CheckRunRecorder path: no
--    check run ever executed, so every row is an all-zero artifact and no
--    backfill from real executions exists. Windows start accruing from
--    rollout. The table keeps its shape (it has no source_type column), so
--    the truncate is the only rewrite the semantics change needs.
-- ============================================================

ALTER TABLE job_run_control_plane
  ADD COLUMN IF NOT EXISTS scheduled_for TIMESTAMPTZ;

UPDATE slis
SET source_type = 'job_run',
    source_config = jsonb_build_object('source_uid', 'check-' || (source_config ->> 'check_id'))
WHERE source_type = 'check_run';

ALTER TABLE slis
  ALTER COLUMN source_type SET DEFAULT 'job_run';

TRUNCATE sli_snapshots;

COMMIT;

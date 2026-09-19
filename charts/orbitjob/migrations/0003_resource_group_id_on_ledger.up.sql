BEGIN;

-- ============================================================
-- Migration 0003: resource_group_id on the ledger tables
-- ============================================================
--
-- One schema gap closed, in the direction the API gates already enforce
-- (schema-strategy.md G2).
--
-- A group-scoped API key names one resource group, and every resource the key
-- can reach must carry the column that scope filters on. api_keys, checks,
-- slis and slos already do; job_definition_revisions and job_run_control_plane
-- did not. The admin API refuses group-scoped keys on the job and run surfaces
-- outright (resource.RequireUnscoped), so no row was ever written out of
-- scope -- but the schema could not say so, and a future filter upgrade on
-- these surfaces would have needed exactly this column.
--
-- Nullable, like checks.resource_group_id:
--   - a ScheduledJob revision has no group (the CR surface refuses
--     group-scoped keys), and
--   - only a check-sourced revision inherits its check row's group, stamped
--     by the materializer (projection.ApplyCheck), and every run copies its
--     revision's group at insert (CreateOccurrenceForTenant).
--
-- No backfill: rows written before this migration predate group lineage, and
-- the refusal gates that keep group-scoped principals off these surfaces are
-- unchanged.
-- ============================================================

ALTER TABLE job_definition_revisions
  ADD COLUMN IF NOT EXISTS resource_group_id CHAR(26);

ALTER TABLE job_run_control_plane
  ADD COLUMN IF NOT EXISTS resource_group_id CHAR(26);

COMMIT;

BEGIN;

-- ============================================================
-- Schema Cleanup
-- ============================================================
-- 1. Remove 'dispatching' from job_instances status CHECK constraint
--    (status was renamed to 'dispatched' in refactor/dispatched-status)
-- 2. Change misfire_policy default from 'skip' to 'fire_now'
--    (consistent with Quartz/Airflow — fire once immediately on miss)

-- Phase 1: Update job_instances status constraint
ALTER TABLE job_instances DROP CONSTRAINT IF EXISTS chk_instances_status;
ALTER TABLE job_instances ADD CONSTRAINT chk_instances_status CHECK (
  status IN (
    'pending',
    'dispatched',
    'running',
    'retry_wait',
    'success',
    'failed',
    'canceled'
  )
);

-- Phase 2: Update misfire_policy default
ALTER TABLE jobs ALTER COLUMN misfire_policy SET DEFAULT 'fire_now';

-- Phase 3: Update existing rows that use the old default
UPDATE jobs SET misfire_policy = 'fire_now' WHERE misfire_policy = 'skip';

COMMIT;

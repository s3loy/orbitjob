-- Revert all 0002 enhancements.

-- 4. Revert instance lease indexes.
DROP INDEX IF EXISTS idx_instances_dispatched_lease;
DROP INDEX IF EXISTS idx_instances_running_lease;

-- 3. Revert retry defaults.
ALTER TABLE jobs
    ALTER COLUMN retry_limit SET DEFAULT 0;

ALTER TABLE job_instances
    ALTER COLUMN max_attempt SET DEFAULT 1;

-- 2. Drop missing indexes.
DROP INDEX IF EXISTS idx_instances_partition_key;
DROP INDEX IF EXISTS idx_instances_routing_key;
DROP INDEX IF EXISTS idx_instances_job_id;

-- 1. Revert schema cleanup.
UPDATE jobs SET misfire_policy = 'skip' WHERE misfire_policy = 'fire_now';
ALTER TABLE jobs ALTER COLUMN misfire_policy SET DEFAULT 'skip';

ALTER TABLE job_instances DROP CONSTRAINT IF EXISTS chk_instances_status;
ALTER TABLE job_instances ADD CONSTRAINT chk_instances_status CHECK (
  status IN (
    'pending',
    'dispatching',
    'dispatched',
    'running',
    'retry_wait',
    'success',
    'failed',
    'canceled'
  )
);

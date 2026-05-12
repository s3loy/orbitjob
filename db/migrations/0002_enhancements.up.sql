-- Schema enhancements applied after initial init.
-- Combines: schema cleanup, missing indexes, retry defaults, instance lease indexes.

-- 1. Schema cleanup: remove 'dispatching' from status CHECK, update misfire_policy default.
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

ALTER TABLE jobs ALTER COLUMN misfire_policy SET DEFAULT 'fire_now';
UPDATE jobs SET misfire_policy = 'fire_now' WHERE misfire_policy = 'skip';

-- 2. Missing indexes for query patterns.
CREATE INDEX IF NOT EXISTS idx_instances_partition_key
    ON job_instances(tenant_id, partition_key)
    WHERE partition_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_instances_routing_key
    ON job_instances(tenant_id, routing_key)
    WHERE routing_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_instances_job_id
    ON job_instances(job_id);

-- 3. Fix default retry settings so retry mechanism works out of the box.
-- Before: retry_limit=0, max_attempt=1 → attempt < max_attempt is always false (1 < 1).
-- After:  retry_limit=2, max_attempt=3 → allows 2 retries (3 attempts total).
ALTER TABLE jobs
    ALTER COLUMN retry_limit SET DEFAULT 2;

ALTER TABLE job_instances
    ALTER COLUMN max_attempt SET DEFAULT 3;

-- 4. Critical composite indexes for orphan recovery and lease scanning.
CREATE INDEX IF NOT EXISTS idx_instances_dispatched_lease
    ON job_instances(status, lease_expires_at)
    WHERE status = 'dispatched';

CREATE INDEX IF NOT EXISTS idx_instances_running_lease
    ON job_instances(status, lease_expires_at)
    WHERE status = 'running';

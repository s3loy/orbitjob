-- Add critical composite indexes for orphan recovery and lease scanning.
-- These prevent Seq Scan on job_instances at scale.

CREATE INDEX IF NOT EXISTS idx_instances_dispatched_lease
    ON job_instances(status, lease_expires_at)
    WHERE status = 'dispatched';

CREATE INDEX IF NOT EXISTS idx_instances_running_lease
    ON job_instances(status, lease_expires_at)
    WHERE status = 'running';

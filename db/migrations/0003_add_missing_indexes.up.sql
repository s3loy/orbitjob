-- Add missing indexes for query patterns discovered from production workload analysis.
-- 2026-05-11 audit.

-- Index for worker sharding / partition key queries.
CREATE INDEX IF NOT EXISTS idx_instances_partition_key
ON job_instances(tenant_id, partition_key)
WHERE partition_key IS NOT NULL;

-- Index for label-based routing queries.
CREATE INDEX IF NOT EXISTS idx_instances_routing_key
ON job_instances(tenant_id, routing_key)
WHERE routing_key IS NOT NULL;

-- Standalone index on job_id for cross-tenant lookups and dashboard queries.
CREATE INDEX IF NOT EXISTS idx_instances_job_id
ON job_instances(job_id);

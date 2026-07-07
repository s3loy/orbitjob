-- Scheduler leader fencing token table.
-- Stores the current valid leader epoch per tenant.
-- Old rows are automatically cleaned up by lease expiry.
CREATE TABLE IF NOT EXISTS scheduler_leaders (
    tenant_id TEXT PRIMARY KEY,
    leader_epoch BIGINT NOT NULL,
    leader_id TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

-- Automatic cleanup of expired rows via background worker (PG 13+).
-- For older PG versions, rely on application-layer pruning or cron job.
CREATE INDEX IF NOT EXISTS idx_scheduler_leaders_expires_at ON scheduler_leaders(expires_at);

-- Add container jobs executed as isolated Kubernetes batch/v1 Jobs.
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS chk_jobs_handler_type;
ALTER TABLE jobs ADD CONSTRAINT chk_jobs_handler_type
    CHECK (handler_type IN ('exec', 'http', 'webhook', 'pg_notify', 'container'));

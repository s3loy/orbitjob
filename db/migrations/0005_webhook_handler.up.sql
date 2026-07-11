-- Expand handler_type constraint to include webhook and pg_notify.
-- Both handlers are fully implemented in internal/core/app/execute/handler/
-- and registered in cmd/worker/main.go buildRunnerFn.
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS chk_jobs_handler_type;
ALTER TABLE jobs ADD CONSTRAINT chk_jobs_handler_type
    CHECK (handler_type IN ('exec', 'http', 'webhook', 'pg_notify'));

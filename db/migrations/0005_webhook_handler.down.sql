-- Revert to original handler_type constraint (exec, http only).
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS chk_jobs_handler_type;
ALTER TABLE jobs ADD CONSTRAINT chk_jobs_handler_type
    CHECK (handler_type IN ('exec', 'http'));

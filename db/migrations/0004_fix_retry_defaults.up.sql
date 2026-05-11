-- Fix default retry settings so retry mechanism works out of the box.
-- Before: retry_limit=0, max_attempt=1 → attempt < max_attempt is always false (1 < 1).
-- After:  retry_limit=2, max_attempt=3 → allows 2 retries (3 attempts total).

ALTER TABLE jobs
    ALTER COLUMN retry_limit SET DEFAULT 2;

ALTER TABLE job_instances
    ALTER COLUMN max_attempt SET DEFAULT 3;

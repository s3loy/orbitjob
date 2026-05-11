-- Revert retry defaults to original values (disables retries by default).

ALTER TABLE jobs
    ALTER COLUMN retry_limit SET DEFAULT 0;

ALTER TABLE job_instances
    ALTER COLUMN max_attempt SET DEFAULT 1;

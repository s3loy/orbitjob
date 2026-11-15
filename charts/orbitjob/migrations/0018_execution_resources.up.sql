BEGIN;

-- Existing Function versions remain legacy. New protocol values are saved by
-- the authorized management write and included in each immutable revision.
ALTER TABLE functions
  ADD COLUMN execution_policy_version INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN retry_safe BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN resources JSONB,
  ADD CONSTRAINT chk_function_execution_policy_version
    CHECK (execution_policy_version IN (0, 1)),
  ADD CONSTRAINT chk_function_resources_object
    CHECK (resources IS NULL OR jsonb_typeof(resources) = 'object'),
  ADD CONSTRAINT chk_function_v1_retry_safe
    CHECK (execution_policy_version = 0 OR retry_limit = 0 OR retry_safe),
  ADD CONSTRAINT chk_function_v1_resolved_resources
    CHECK (execution_policy_version = 0 OR resources IS NOT NULL);

-- No ownership, RLS or role grant changes. Runtime still reads Function
-- configuration; Admin management writes and audit remain transactional.
COMMIT;

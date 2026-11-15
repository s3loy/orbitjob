BEGIN;

-- Existing Checks retain their original pinned projection. Ordinary new
-- Check creation uses policy 1; upgrading an existing Check is explicit and
-- increments its configuration version before a new revision is projected.
ALTER TABLE checks ADD COLUMN execution_policy_version SMALLINT NOT NULL DEFAULT 0
 CHECK(execution_policy_version IN(0,1));
ALTER TABLE checks ALTER COLUMN execution_policy_version SET DEFAULT 1;

COMMIT;

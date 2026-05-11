-- Revert instance lease indexes.

DROP INDEX IF EXISTS idx_instances_dispatched_lease;
DROP INDEX IF EXISTS idx_instances_running_lease;

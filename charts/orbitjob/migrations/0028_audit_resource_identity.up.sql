-- Definition audit records retain namespace/name, which can exceed 64 characters.
-- Widen the partitioned parent and its partitions without changing existing rows,
-- tenant policies, indexes or application privileges.
ALTER TABLE audit_events ALTER COLUMN resource_id TYPE TEXT;

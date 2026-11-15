BEGIN;

-- A pre-persistence admission identity is evidence of the original object,
-- not proof that Kubernetes committed the write. It prevents cloned metadata
-- on a recreated object from confirming an unknown creation.
ALTER TABLE definition_operations ADD COLUMN admitted_uid TEXT;
ALTER TABLE definition_operations ADD CONSTRAINT definition_admitted_uid_bound
  CHECK (admitted_uid IS NULL OR (length(admitted_uid)>0 AND length(admitted_uid)<=128));
GRANT UPDATE (admitted_uid) ON definition_operations TO orbitjob_runtime;

COMMIT;

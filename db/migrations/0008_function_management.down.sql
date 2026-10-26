-- Preserve Function workloads and history when withdrawing management.
UPDATE policies
SET document = jsonb_set(document, '{statement,0,action}', (
  SELECT COALESCE(jsonb_agg(action), '[]'::jsonb)
  FROM jsonb_array_elements_text(document #> '{statement,0,action}') AS action
  WHERE action NOT IN ('function:Create', 'function:Update')
))
WHERE id = '00000000000000000000000011'
  AND name = 'TenantAdminAccess' AND tenant_id IS NULL;

DROP INDEX IF EXISTS uq_functions_create_receipt;
ALTER TABLE functions DROP CONSTRAINT IF EXISTS chk_functions_create_receipt;
ALTER TABLE functions
  DROP COLUMN IF EXISTS create_actor_id,
  DROP COLUMN IF EXISTS create_idempotency_key,
  DROP COLUMN IF EXISTS create_payload_hash;

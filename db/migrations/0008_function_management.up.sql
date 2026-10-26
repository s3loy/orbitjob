-- ADR0017: stable creation receipts live beside Function definitions.
-- Older definitions remain valid; only managed creates carry this metadata.
ALTER TABLE functions
  ADD COLUMN create_actor_id VARCHAR(64),
  ADD COLUMN create_idempotency_key VARCHAR(128),
  ADD COLUMN create_payload_hash CHAR(64),
  ADD CONSTRAINT chk_functions_create_receipt CHECK (
    (create_actor_id IS NULL AND create_idempotency_key IS NULL AND create_payload_hash IS NULL)
    OR (create_actor_id IS NOT NULL AND create_idempotency_key IS NOT NULL AND create_payload_hash IS NOT NULL)
  );

CREATE UNIQUE INDEX uq_functions_create_receipt
  ON functions (tenant_id, create_actor_id, create_idempotency_key)
  WHERE create_idempotency_key IS NOT NULL;

-- Extend only the named platform preset. Explicit tenant policies keep their
-- previous capabilities; no read or invocation permission implies writes.
UPDATE policies
SET document = jsonb_set(document, '{statement,0,action}', (
  SELECT jsonb_agg(action ORDER BY action)
  FROM (
    SELECT DISTINCT jsonb_array_elements_text(document #> '{statement,0,action}') AS action
    UNION SELECT 'function:Create'
    UNION SELECT 'function:Update'
  ) actions
))
WHERE id = '00000000000000000000000011'
  AND name = 'TenantAdminAccess' AND tenant_id IS NULL;

-- ADR0019: optional display metadata never participates in authentication.
ALTER TABLE api_keys ADD COLUMN name VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD CONSTRAINT chk_api_keys_name CHECK (
  name = btrim(name) AND name !~ '[[:cntrl:]]'
);

-- Give the tenant administration preset the independently guarded metadata
-- operation. Custom policies retain their existing explicit capabilities.
UPDATE policies
SET document = jsonb_set(document, '{statement,0,action}', (
  SELECT jsonb_agg(action ORDER BY action)
  FROM (
    SELECT DISTINCT jsonb_array_elements_text(document #> '{statement,0,action}') AS action
    UNION SELECT 'apikey:Update'
  ) actions
))
WHERE id = '00000000000000000000000011'
  AND name = 'TenantAdminAccess' AND tenant_id IS NULL;

UPDATE policies
SET document = jsonb_set(document, '{statement,0,action}', (
  SELECT COALESCE(jsonb_agg(action), '[]'::jsonb)
  FROM jsonb_array_elements_text(document #> '{statement,0,action}') AS action
  WHERE action <> 'apikey:Update'
))
WHERE id = '00000000000000000000000011'
  AND name = 'TenantAdminAccess' AND tenant_id IS NULL;

ALTER TABLE api_keys DROP COLUMN IF EXISTS name;

BEGIN;

-- Configuration drafts are ciphertext, scoped to the authenticated key as well
-- as the tenant. No API credential or checked-save proof belongs in this data.
CREATE TABLE private_authoring_drafts (
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  owner_id TEXT NOT NULL CHECK(length(owner_id) BETWEEN 1 AND 64),
  kind TEXT NOT NULL CHECK(kind IN ('jobs','workflows')),
  namespace TEXT NOT NULL CHECK(length(namespace) BETWEEN 1 AND 63),
  name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 253),
  draft_id UUID NOT NULL,
  key_id TEXT NOT NULL CHECK(length(key_id) BETWEEN 1 AND 64),
  ciphertext BYTEA NOT NULL CHECK(octet_length(ciphertext) BETWEEN 16 AND 524288),
  nonce BYTEA NOT NULL CHECK(octet_length(nonce)=12),
  version BIGINT NOT NULL CHECK(version>0),
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY(tenant_id,owner_id,kind,namespace,name),
  UNIQUE(tenant_id,draft_id)
);
ALTER TABLE private_authoring_drafts OWNER TO orbitjob_table_owner;
ALTER TABLE private_authoring_drafts ENABLE ROW LEVEL SECURITY;
CREATE POLICY private_draft_subject ON private_authoring_drafts
  FOR ALL USING (tenant_id=current_setting('app.tenant_id',true) AND owner_id=current_setting('app.actor_id',true))
  WITH CHECK (tenant_id=current_setting('app.tenant_id',true) AND owner_id=current_setting('app.actor_id',true));
CREATE INDEX private_draft_expiry ON private_authoring_drafts(tenant_id,owner_id,expires_at);
GRANT SELECT,INSERT,UPDATE,DELETE ON private_authoring_drafts TO orbitjob_admin;

-- Recovery is explicit and independently granted; regular Apply cannot read
-- another key's private draft. Custom policies do not gain these capabilities.
UPDATE policies SET document=jsonb_set(document,'{statement,0,action}',(
  SELECT jsonb_agg(action ORDER BY action) FROM (
    SELECT DISTINCT jsonb_array_elements_text(document #> '{statement,0,action}') AS action
    UNION SELECT 'job:RecoverDraft' UNION SELECT 'workflow:RecoverDraft'
  ) actions))
WHERE id='00000000000000000000000011' AND name='TenantAdminAccess' AND tenant_id IS NULL;

COMMIT;

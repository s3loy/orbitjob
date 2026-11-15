BEGIN;

-- Admin-owned authoring records do not grant execution-ledger write access.
-- Request identities remain as compact tombstones after detail retention.
CREATE TABLE definition_operations (
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  request_id UUID NOT NULL,
  actor_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('jobs','workflows')),
  namespace TEXT NOT NULL,
  name TEXT NOT NULL,
  payload_hash CHAR(64) NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('checked','dispatching','accepted','rejected','unknown','expired')),
  request_json JSONB NOT NULL,
  result_json JSONB,
  checked_until TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id,request_id),
  CHECK (pg_column_size(request_json)<=524288),
  CHECK (result_json IS NULL OR pg_column_size(result_json)<=524288)
);
ALTER TABLE definition_operations OWNER TO orbitjob_table_owner;
ALTER TABLE definition_operations ENABLE ROW LEVEL SECURITY;
CREATE POLICY definition_operations_tenant ON definition_operations
  FOR ALL USING (tenant_id=current_setting('app.tenant_id',true))
  WITH CHECK (tenant_id=current_setting('app.tenant_id',true));
CREATE INDEX definition_operations_actor ON definition_operations (tenant_id,actor_id,created_at DESC);
GRANT SELECT,INSERT,UPDATE ON definition_operations TO orbitjob_admin;
-- Runtime reads only immutable checked intent when enforcing final admission.
GRANT SELECT ON definition_operations TO orbitjob_runtime;

COMMIT;

BEGIN;

ALTER TABLE job_run_attempts_control_plane ADD COLUMN log_resources_cleaned_at TIMESTAMPTZ;

-- ADR0028/0029: compact tenant-scoped archive identities outlive Kubernetes
-- resources. Container output and per-line events live on the bounded log PVC.
CREATE TABLE IF NOT EXISTS execution_log_archives (
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  archive_id CHAR(64) NOT NULL,
  run_id BIGINT NOT NULL DEFAULT 0,
  function_id BIGINT NOT NULL DEFAULT 0,
  function_run_id UUID,
  identity_json JSONB NOT NULL,
  state VARCHAR(24) NOT NULL DEFAULT 'collecting',
  protected_until TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  cleanup_requested_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, archive_id),
  CONSTRAINT chk_log_archive_identity CHECK
    (jsonb_typeof(identity_json)='object' AND pg_column_size(identity_json)<=8192),
  CONSTRAINT chk_log_archive_state CHECK
    (state IN ('collecting','sealed','partial','capacity','expired','deleted'))
);
ALTER TABLE execution_log_archives OWNER TO orbitjob_table_owner;
ALTER TABLE execution_log_archives ENABLE ROW LEVEL SECURITY;
CREATE POLICY execution_log_archive_tenant ON execution_log_archives
  FOR ALL USING (tenant_id=current_setting('app.tenant_id',true))
  WITH CHECK (tenant_id=current_setting('app.tenant_id',true));
CREATE INDEX idx_log_archive_run ON execution_log_archives (tenant_id,run_id);
CREATE INDEX idx_log_archive_function ON execution_log_archives (tenant_id,function_id,function_run_id);
GRANT SELECT ON execution_log_archives TO orbitjob_admin;
GRANT SELECT,INSERT,UPDATE,DELETE ON execution_log_archives TO orbitjob_runtime;
-- HTTP can request cleanup and commit its audit atomically, not mutate identity.
GRANT UPDATE (cleanup_requested_at) ON execution_log_archives TO orbitjob_admin;

-- Serialize metadata growth across tenants. Owner bypass is confined to the
-- capacity boolean; no cross-tenant identities or output are returned.
CREATE OR REPLACE FUNCTION bound_execution_log_metadata() RETURNS TRIGGER
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN
  IF TG_OP='UPDATE' AND NEW.identity_json=OLD.identity_json THEN RETURN NEW; END IF;
  IF TG_OP='INSERT' AND EXISTS(SELECT 1 FROM public.execution_log_archives e
     WHERE e.tenant_id=NEW.tenant_id AND e.archive_id=NEW.archive_id
       AND e.identity_json=NEW.identity_json) THEN RETURN NEW; END IF;
  PERFORM pg_advisory_xact_lock(32117,260426);
  IF pg_total_relation_size('public.execution_log_archives')+32768>268435456
     OR (SELECT COALESCE(sum(pg_column_size(identity_json)),0)
         FROM public.execution_log_archives)+8192>134217728 THEN
    RAISE EXCEPTION 'execution log metadata capacity exhausted' USING ERRCODE='53000';
  END IF;
  RETURN NEW;
END $$;
ALTER FUNCTION bound_execution_log_metadata() OWNER TO orbitjob_table_owner;
REVOKE ALL ON FUNCTION bound_execution_log_metadata() FROM PUBLIC;
CREATE TRIGGER bound_execution_log_metadata
  BEFORE INSERT OR UPDATE OF identity_json ON execution_log_archives
  FOR EACH ROW EXECUTE FUNCTION bound_execution_log_metadata();

-- Extend the named administrator preset only; custom policies stay unchanged.
UPDATE policies SET document=jsonb_set(document,'{statement,0,action}',(
  SELECT jsonb_agg(action ORDER BY action) FROM (
    SELECT DISTINCT jsonb_array_elements_text(document #> '{statement,0,action}') AS action
    UNION SELECT 'instance:DeleteLogs' UNION SELECT 'function:DeleteLogs'
  ) actions))
WHERE id='00000000000000000000000011'
  AND name='TenantAdminAccess' AND tenant_id IS NULL;

COMMIT;

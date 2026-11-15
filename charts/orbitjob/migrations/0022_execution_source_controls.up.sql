BEGIN;

CREATE TABLE execution_source_controls (
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  source_mode VARCHAR(16) NOT NULL CHECK(source_mode IN ('kubernetes','workflow','function','check')),
  source_uid VARCHAR(128) NOT NULL CHECK(btrim(source_uid) <> ''),
  version BIGINT NOT NULL DEFAULT 1 CHECK(version > 0),
  schedule_paused BOOLEAN NOT NULL DEFAULT false,
  execution_paused BOOLEAN NOT NULL DEFAULT false,
  tombstoned BOOLEAN NOT NULL DEFAULT false,
  actor VARCHAR(255) NOT NULL CHECK(btrim(actor) <> ''),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(tenant_id,source_mode,source_uid)
);
ALTER TABLE execution_source_controls ENABLE ROW LEVEL SECURITY;
CREATE POLICY execution_source_controls_tenant ON execution_source_controls
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING(tenant_id=current_setting('app.tenant_id',true))
  WITH CHECK(tenant_id=current_setting('app.tenant_id',true));
GRANT SELECT,INSERT,UPDATE ON execution_source_controls TO orbitjob_admin,orbitjob_runtime,orbitjob_operator;

CREATE FUNCTION guard_source_control_identity() RETURNS TRIGGER
LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
BEGIN
  IF (NEW.tenant_id,NEW.source_mode,NEW.source_uid) IS DISTINCT FROM
     (OLD.tenant_id,OLD.source_mode,OLD.source_uid) OR
     (OLD.tombstoned AND NOT NEW.tombstoned) OR NEW.version <> OLD.version+1 THEN
    RAISE EXCEPTION 'source control identity and deletion are immutable; version must advance';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_source_control_identity() FROM PUBLIC;
CREATE TRIGGER guard_source_control_identity BEFORE UPDATE ON execution_source_controls
  FOR EACH ROW EXECUTE FUNCTION guard_source_control_identity();

-- Existing activation instants cannot be reconstructed. New activations pin a
-- wall-clock lower bound while the source admission lock is held.
ALTER TABLE job_definition_revisions ADD COLUMN activated_at TIMESTAMPTZ;

-- Runtime cannot update tenants. This narrow capability only locks its tenant
-- row for admission, so suspension cannot commit across a successful marker.
CREATE FUNCTION execution_tenant_active() RETURNS BOOLEAN
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE state TEXT;
BEGIN
  SELECT status INTO state FROM public.tenants
    WHERE id=current_setting('app.tenant_id',true) FOR SHARE;
  RETURN COALESCE(state='active',false);
END $$;
REVOKE ALL ON FUNCTION execution_tenant_active() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION execution_tenant_active() TO orbitjob_runtime;

COMMIT;

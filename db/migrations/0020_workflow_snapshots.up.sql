BEGIN;

-- Historical runs retain late binding. Only newly accepted version 1 runs
-- receive a complete snapshot; no migration can reconstruct past choices.
ALTER TABLE workflow_run_control_plane
  ADD COLUMN snapshot_version INTEGER NOT NULL DEFAULT 0
  CHECK (snapshot_version IN (0,1));

CREATE TABLE workflow_task_snapshots (
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  workflow_run_id BIGINT NOT NULL REFERENCES workflow_run_control_plane(id) ON DELETE CASCADE,
  task_name TEXT NOT NULL CHECK (btrim(task_name) <> ''),
  source_uid VARCHAR(128) NOT NULL,
  revision_id BIGINT NOT NULL REFERENCES job_definition_revisions(id) ON DELETE RESTRICT,
  occurrence_key CHAR(64) NOT NULL,
  PRIMARY KEY (workflow_run_id, task_name),
  UNIQUE (workflow_run_id, occurrence_key)
);
CREATE INDEX workflow_task_snapshots_revision ON workflow_task_snapshots(revision_id);
ALTER TABLE workflow_task_snapshots ENABLE ROW LEVEL SECURITY;
CREATE POLICY workflow_task_snapshots_tenant ON workflow_task_snapshots
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));
GRANT SELECT ON workflow_task_snapshots TO orbitjob_admin;
GRANT SELECT, INSERT ON workflow_task_snapshots TO orbitjob_runtime, orbitjob_operator;

CREATE FUNCTION guard_workflow_snapshot_version() RETURNS TRIGGER
LANGUAGE plpgsql SET search_path = pg_catalog, public AS $$
BEGIN
  IF NEW.snapshot_version <> OLD.snapshot_version OR (OLD.snapshot_version=1 AND
     (NEW.tenant_id,NEW.source_uid,NEW.revision_id,NEW.occurrence_key) IS DISTINCT FROM
     (OLD.tenant_id,OLD.source_uid,OLD.revision_id,OLD.occurrence_key)) THEN
    RAISE EXCEPTION 'workflow snapshot identity is immutable';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_workflow_snapshot_version() FROM PUBLIC;
CREATE TRIGGER guard_workflow_snapshot_version BEFORE UPDATE OF snapshot_version,tenant_id,source_uid,revision_id,occurrence_key
  ON workflow_run_control_plane FOR EACH ROW EXECUTE FUNCTION guard_workflow_snapshot_version();

-- A deferred check makes a parent and its entire task reference set one fact.
-- The revision FK retains not-yet-created tasks' history until the run is pruned.
CREATE FUNCTION require_complete_workflow_snapshot() RETURNS TRIGGER
LANGUAGE plpgsql SET search_path = pg_catalog, public AS $$
DECLARE expected INTEGER; actual INTEGER;
BEGIN
  IF NEW.snapshot_version = 1 THEN
    SELECT jsonb_array_length(r.normalized_spec->'tasks') INTO expected
      FROM job_definition_revisions r WHERE r.id=NEW.revision_id
      AND r.tenant_id=NEW.tenant_id AND r.source_mode='workflow'
      AND r.source_uid=NEW.source_uid
      AND r.normalized_spec->>'executionPolicyVersion'='1';
    SELECT count(*) INTO actual FROM workflow_task_snapshots s
      WHERE s.workflow_run_id=NEW.id AND s.tenant_id=NEW.tenant_id;
    IF expected IS NULL OR expected=0 OR actual <> expected THEN
      RAISE EXCEPTION 'workflow snapshot must contain every declared task';
    END IF;
  END IF;
  RETURN NULL;
END $$;
REVOKE ALL ON FUNCTION require_complete_workflow_snapshot() FROM PUBLIC;
CREATE CONSTRAINT TRIGGER require_complete_workflow_snapshot
  AFTER INSERT ON workflow_run_control_plane DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION require_complete_workflow_snapshot();

CREATE FUNCTION guard_workflow_task_snapshot() RETURNS TRIGGER
LANGUAGE plpgsql SET search_path = pg_catalog, public AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM workflow_run_control_plane w
    JOIN job_definition_revisions wr ON wr.id=w.revision_id AND wr.tenant_id=w.tenant_id
    JOIN job_definition_revisions r ON r.id=NEW.revision_id AND r.tenant_id=w.tenant_id
    CROSS JOIN LATERAL jsonb_array_elements(wr.normalized_spec->'tasks') t
    WHERE w.id=NEW.workflow_run_id AND w.tenant_id=NEW.tenant_id AND w.snapshot_version=1
    AND t->>'name'=NEW.task_name AND t->'jobRef'->>'name'=r.source_name
    AND r.source_mode='kubernetes' AND r.source_uid=NEW.source_uid
    AND r.source_namespace=wr.source_namespace
    AND NEW.occurrence_key=encode(sha256(convert_to('wfstep|' || w.source_uid || '|' || rtrim(w.occurrence_key) || '|' || NEW.task_name,'UTF8')),'hex')
  ) THEN
    RAISE EXCEPTION 'workflow task snapshot identity does not match the pinned graph';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_workflow_task_snapshot() FROM PUBLIC;
CREATE TRIGGER guard_workflow_task_snapshot BEFORE INSERT ON workflow_task_snapshots
  FOR EACH ROW EXECUTE FUNCTION guard_workflow_task_snapshot();

CREATE FUNCTION guard_workflow_snapshot_step() RETURNS TRIGGER
LANGUAGE plpgsql SET search_path = pg_catalog, public AS $$
BEGIN
  IF NEW.workflow_run_id IS NOT NULL AND EXISTS (
    SELECT 1 FROM workflow_run_control_plane w
    WHERE w.id=NEW.workflow_run_id AND w.tenant_id=NEW.tenant_id AND w.snapshot_version=1
  ) AND NOT EXISTS (
    SELECT 1 FROM workflow_task_snapshots s
    WHERE s.workflow_run_id=NEW.workflow_run_id AND s.tenant_id=NEW.tenant_id
    AND s.source_uid=NEW.source_uid AND s.revision_id=NEW.revision_id
    AND s.occurrence_key=NEW.occurrence_key
  ) THEN
    RAISE EXCEPTION 'workflow step does not match its accepted snapshot';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_workflow_snapshot_step() FROM PUBLIC;
CREATE TRIGGER guard_workflow_snapshot_step
  BEFORE INSERT OR UPDATE OF workflow_run_id, source_uid, revision_id, occurrence_key
  ON job_run_control_plane FOR EACH ROW EXECUTE FUNCTION guard_workflow_snapshot_step();

COMMIT;

BEGIN;

-- Stop old writers before this cutover. Existing Runs and aggregates keep
-- processing-time semantics; only newly accepted Runs use event-time matching.
ALTER TABLE job_run_control_plane
  ADD COLUMN sli_semantics_version SMALLINT NOT NULL DEFAULT 1
    CHECK (sli_semantics_version IN (1,2)),
  ADD COLUMN outcome_observed_at TIMESTAMPTZ;
UPDATE job_run_control_plane SET outcome_observed_at=updated_at
 WHERE phase IN ('Succeeded','Failed','Canceled');
ALTER TABLE job_run_control_plane ALTER COLUMN sli_semantics_version SET DEFAULT 2;

CREATE FUNCTION public.orbitjob_stamp_terminal_observation() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, public AS $$
BEGIN
  IF NEW.phase IN ('Succeeded','Failed','Canceled') THEN
    IF TG_OP='INSERT' THEN
      NEW.outcome_observed_at := clock_timestamp();
    ELSIF OLD.outcome_observed_at IS NULL THEN
      NEW.outcome_observed_at := clock_timestamp();
    ELSE
      NEW.outcome_observed_at := OLD.outcome_observed_at;
    END IF;
  ELSIF TG_OP='UPDATE' THEN
    NEW.outcome_observed_at := OLD.outcome_observed_at;
  ELSE
    NEW.outcome_observed_at := NULL;
  END IF;
  RETURN NEW;
END $$;
ALTER FUNCTION public.orbitjob_stamp_terminal_observation() OWNER TO orbitjob_table_owner;
REVOKE ALL ON FUNCTION public.orbitjob_stamp_terminal_observation() FROM PUBLIC;
CREATE TRIGGER stamp_terminal_observation BEFORE INSERT OR UPDATE ON job_run_control_plane
 FOR EACH ROW EXECUTE FUNCTION public.orbitjob_stamp_terminal_observation();

CREATE FUNCTION public.orbitjob_lock_sli_source(tenant text, source text) RETURNS void
LANGUAGE sql SET search_path = pg_catalog, public AS $$
 SELECT pg_advisory_xact_lock(hashtextextended('sli-source:' || tenant || ':' || source,0))
$$;
ALTER FUNCTION public.orbitjob_lock_sli_source(text,text) OWNER TO orbitjob_table_owner;
REVOKE ALL ON FUNCTION public.orbitjob_lock_sli_source(text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.orbitjob_lock_sli_source(text,text)
 TO orbitjob_admin,orbitjob_runtime,orbitjob_operator;

CREATE TABLE sli_configuration_versions (
 tenant_id CHAR(26) NOT NULL,
 sli_id BIGINT NOT NULL,
 configuration_version INT NOT NULL CHECK(configuration_version>0),
 definition_json JSONB NOT NULL CHECK(jsonb_typeof(definition_json)='object'),
 effective_from TIMESTAMPTZ NOT NULL,
 effective_until TIMESTAMPTZ,
 PRIMARY KEY(tenant_id,sli_id,configuration_version),
 FOREIGN KEY(tenant_id,sli_id) REFERENCES slis(tenant_id,id) ON DELETE RESTRICT,
 CHECK(effective_until IS NULL OR effective_until>=effective_from)
);
-- Historical eligibility is not reconstructed. The original row remains in
-- definition_json, while v2 eligibility begins at this explicit cutover.
WITH cutover AS (SELECT clock_timestamp() AS at)
INSERT INTO sli_configuration_versions
 SELECT s.tenant_id,s.id,1,to_jsonb(s),c.at,
 CASE WHEN s.deleted_at IS NULL THEN NULL ELSE c.at END
 FROM slis s CROSS JOIN cutover c;

CREATE TABLE sli_observation_state (
 tenant_id CHAR(26) NOT NULL,
 sli_id BIGINT NOT NULL,
 statistics_semantics VARCHAR(32) NOT NULL CHECK(statistics_semantics IN
  ('legacy_processing_v1','event_time_v2','mixed')),
 last_event_at TIMESTAMPTZ,
 last_sample_at TIMESTAMPTZ,
 last_observed_at TIMESTAMPTZ,
 PRIMARY KEY(tenant_id,sli_id),
 FOREIGN KEY(tenant_id,sli_id) REFERENCES slis(tenant_id,id) ON DELETE RESTRICT
);
INSERT INTO sli_observation_state(tenant_id,sli_id,statistics_semantics,last_observed_at)
 SELECT s.tenant_id,s.id,
 CASE WHEN count(p.id)>0 THEN 'legacy_processing_v1' ELSE 'event_time_v2' END,
 max(p.updated_at)
 FROM slis s LEFT JOIN sli_snapshots p ON p.tenant_id=s.tenant_id AND p.sli_id=s.id
 GROUP BY s.tenant_id,s.id;

CREATE FUNCTION public.orbitjob_sli_configuration_boundary() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE at timestamptz;
BEGIN
 IF TG_OP='INSERT' THEN
  PERFORM public.orbitjob_lock_sli_source(NEW.tenant_id::text,COALESCE(NEW.source_config->>'source_uid',''));
  IF COALESCE(NEW.source_config->>'source_uid','')<>'' AND
     (SELECT count(*) FROM slis WHERE tenant_id=NEW.tenant_id AND deleted_at IS NULL
      AND source_config->>'source_uid'=NEW.source_config->>'source_uid')>=256 THEN
   RAISE EXCEPTION 'an execution source supports at most 256 active SLIs'
    USING ERRCODE='23514',CONSTRAINT='slis_source_capacity';
  END IF;
  NEW.created_at := clock_timestamp();
  NEW.updated_at := NEW.created_at;
  RETURN NEW;
 END IF;
 IF ROW(NEW.tenant_id,NEW.sli_type,NEW.source_type,NEW.source_config,
        NEW.aggregation,NEW.good_event_criteria,NEW.resource_group_id)
    IS DISTINCT FROM ROW(OLD.tenant_id,OLD.sli_type,OLD.source_type,OLD.source_config,
        OLD.aggregation,OLD.good_event_criteria,OLD.resource_group_id) THEN
  RAISE EXCEPTION 'SLI execution criteria are immutable; create a new SLI' USING ERRCODE='23514';
 END IF;
 IF OLD.deleted_at IS NOT NULL AND NEW.deleted_at IS DISTINCT FROM OLD.deleted_at THEN
  RAISE EXCEPTION 'a deleted SLI cannot be reactivated' USING ERRCODE='23514';
 END IF;
 IF OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
  PERFORM public.orbitjob_lock_sli_source(OLD.tenant_id::text,COALESCE(OLD.source_config->>'source_uid',''));
  SELECT greatest(clock_timestamp(),effective_from) INTO at
   FROM sli_configuration_versions WHERE tenant_id=OLD.tenant_id AND sli_id=OLD.id AND configuration_version=1;
  NEW.deleted_at := at;
  NEW.updated_at := at;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION public.orbitjob_sli_configuration_boundary() OWNER TO orbitjob_table_owner;
REVOKE ALL ON FUNCTION public.orbitjob_sli_configuration_boundary() FROM PUBLIC;
CREATE TRIGGER sli_configuration_boundary BEFORE INSERT OR UPDATE ON slis
 FOR EACH ROW EXECUTE FUNCTION public.orbitjob_sli_configuration_boundary();

CREATE FUNCTION public.orbitjob_store_sli_configuration() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  INSERT INTO sli_configuration_versions VALUES(NEW.tenant_id,NEW.id,1,to_jsonb(NEW),NEW.created_at,NULL);
  INSERT INTO sli_observation_state(tenant_id,sli_id,statistics_semantics)
   VALUES(NEW.tenant_id,NEW.id,'event_time_v2');
 ELSIF OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
  UPDATE sli_configuration_versions SET effective_until=NEW.deleted_at
   WHERE tenant_id=NEW.tenant_id AND sli_id=NEW.id AND configuration_version=1;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION public.orbitjob_store_sli_configuration() OWNER TO orbitjob_table_owner;
REVOKE ALL ON FUNCTION public.orbitjob_store_sli_configuration() FROM PUBLIC;
CREATE TRIGGER store_sli_configuration AFTER INSERT OR UPDATE ON slis
 FOR EACH ROW EXECUTE FUNCTION public.orbitjob_store_sli_configuration();

CREATE TABLE sli_event_plans (
 tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
 run_id BIGINT NOT NULL,
 source_uid TEXT NOT NULL,
 semantics VARCHAR(32) NOT NULL CHECK(semantics IN('legacy_processing_v1','event_time_v2')),
 event_time TIMESTAMPTZ NOT NULL,
 time_quality VARCHAR(32) NOT NULL CHECK(time_quality IN('container_finished','observed','legacy_processing')),
 container_started_at TIMESTAMPTZ,
 container_finished_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,run_id),
 CHECK((container_started_at IS NULL AND container_finished_at IS NULL) OR
       (container_started_at IS NOT NULL AND container_finished_at IS NOT NULL AND container_finished_at>=container_started_at))
);
CREATE TABLE sli_event_plan_members (
 tenant_id CHAR(26) NOT NULL,
 run_id BIGINT NOT NULL,
 sli_id BIGINT NOT NULL,
 configuration_version INT NOT NULL,
 PRIMARY KEY(tenant_id,run_id,sli_id),
 FOREIGN KEY(tenant_id,run_id) REFERENCES sli_event_plans(tenant_id,run_id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,sli_id,configuration_version)
  REFERENCES sli_configuration_versions(tenant_id,sli_id,configuration_version) ON DELETE RESTRICT
);

ALTER TABLE sli_snapshot_events
 ALTER COLUMN is_good DROP NOT NULL,
 ADD COLUMN configuration_version INT NOT NULL DEFAULT 1,
 ADD COLUMN semantics VARCHAR(32) NOT NULL DEFAULT 'legacy_processing_v1'
   CHECK(semantics IN('legacy_processing_v1','event_time_v2')),
 ADD COLUMN event_time TIMESTAMPTZ,
 ADD COLUMN time_quality VARCHAR(32) NOT NULL DEFAULT 'legacy_processing',
 ADD COLUMN evaluation_state VARCHAR(16) NOT NULL DEFAULT 'evaluated'
   CHECK(evaluation_state IN('evaluated','unavailable')),
 ADD COLUMN missing_reason VARCHAR(128),
 ADD COLUMN plan_run_id BIGINT,
 ADD CONSTRAINT sli_snapshot_event_evaluation CHECK
  ((evaluation_state='evaluated' AND is_good IS NOT NULL AND missing_reason IS NULL)
   OR (evaluation_state='unavailable' AND is_good IS NULL AND missing_reason IS NOT NULL AND length(missing_reason)>0)),
 ADD CONSTRAINT sli_snapshot_event_plan_required CHECK
  (semantics<>'event_time_v2' OR (plan_run_id IS NOT NULL AND plan_run_id=run_id)),
 ADD FOREIGN KEY(tenant_id,sli_id,configuration_version)
  REFERENCES sli_configuration_versions(tenant_id,sli_id,configuration_version),
 ADD FOREIGN KEY(tenant_id,plan_run_id,sli_id)
  REFERENCES sli_event_plan_members(tenant_id,run_id,sli_id) ON DELETE RESTRICT;
CREATE INDEX sli_event_plans_cleanup ON sli_event_plans(created_at,tenant_id,run_id);

ALTER TABLE sli_snapshots
 ADD COLUMN statistics_semantics VARCHAR(32) NOT NULL DEFAULT 'legacy_processing_v1'
  CHECK(statistics_semantics IN('legacy_processing_v1','event_time_v2','mixed')),
 ADD COLUMN last_event_at TIMESTAMPTZ,
 ADD COLUMN last_sample_at TIMESTAMPTZ,
 ADD COLUMN last_observed_at TIMESTAMPTZ;
ALTER TABLE budgets
 ADD COLUMN evaluated_at TIMESTAMPTZ,
 ADD COLUMN last_event_at TIMESTAMPTZ,
 ADD COLUMN last_sample_at TIMESTAMPTZ,
 ADD COLUMN statistics_semantics VARCHAR(32) NOT NULL DEFAULT 'legacy_processing_v1'
  CHECK(statistics_semantics IN('legacy_processing_v1','event_time_v2','mixed'));
UPDATE budgets SET evaluated_at=updated_at;

DO $$ DECLARE name text; BEGIN
 FOREACH name IN ARRAY ARRAY['sli_configuration_versions','sli_observation_state','sli_event_plans','sli_event_plan_members'] LOOP
  EXECUTE format('ALTER TABLE public.%I OWNER TO orbitjob_table_owner',name);
  EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY',name);
  EXECUTE format('CREATE POLICY tenant_scope ON public.%I FOR ALL TO orbitjob_admin,orbitjob_runtime,orbitjob_operator USING (tenant_id=current_setting(''app.tenant_id'',true)) WITH CHECK (tenant_id=current_setting(''app.tenant_id'',true))',name);
  EXECUTE format('GRANT SELECT ON public.%I TO orbitjob_admin,orbitjob_runtime,orbitjob_operator',name);
 END LOOP;
END $$;
GRANT INSERT,DELETE ON sli_event_plans,sli_event_plan_members TO orbitjob_runtime,orbitjob_operator;
GRANT INSERT,UPDATE ON sli_observation_state TO orbitjob_runtime,orbitjob_operator;
-- Runtime compaction removes facts only after the original ledger Run is gone.
-- Old writers must be stopped: they do not validate the durable plan protocol.
GRANT DELETE ON sli_snapshot_events TO orbitjob_runtime,orbitjob_operator;

COMMIT;

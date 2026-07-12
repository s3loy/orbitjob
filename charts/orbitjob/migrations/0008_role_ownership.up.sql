-- Object ownership is separated from login identities. Login-role attributes and
-- membership are enforced by the owner-init command before this migration runs.
DO $$
DECLARE
  object_name text;
BEGIN
  FOR object_name IN
    SELECT quote_ident(tablename) FROM pg_tables WHERE schemaname = 'public'
  LOOP
    EXECUTE 'ALTER TABLE public.' || object_name || ' OWNER TO orbitjob_table_owner';
  END LOOP;

  FOR object_name IN
    SELECT quote_ident(sequencename) FROM pg_sequences WHERE schemaname = 'public'
  LOOP
    EXECUTE 'ALTER SEQUENCE public.' || object_name || ' OWNER TO orbitjob_table_owner';
  END LOOP;
END $$;

GRANT USAGE ON SCHEMA public TO orbitjob_admin, orbitjob_runtime, orbitjob_operator;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO orbitjob_admin;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO orbitjob_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON jobs, job_instances, workers, checks, check_runs,
  slis, slos, sli_snapshots, budgets, budget_alerts, scheduler_leaders TO orbitjob_runtime;
GRANT SELECT, INSERT ON job_instance_attempts, audit_events, job_change_audits TO orbitjob_runtime;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO orbitjob_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON jobs TO orbitjob_operator;
GRANT SELECT, INSERT ON job_change_audits TO orbitjob_operator;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO orbitjob_operator;

-- The migrator changes schema only by explicitly assuming the NOLOGIN owner.
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO orbitjob_admin, orbitjob_runtime, orbitjob_operator;
GRANT USAGE, CREATE ON SCHEMA public TO orbitjob_table_owner;

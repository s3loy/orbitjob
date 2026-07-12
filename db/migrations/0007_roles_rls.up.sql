-- v0.2.0 database roles and mandatory tenant isolation.
-- Passwords are assigned by deployment tooling; migrations never store secrets.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orbitjob_table_owner') THEN
    CREATE ROLE orbitjob_table_owner NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orbitjob_migrator') THEN
    CREATE ROLE orbitjob_migrator LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orbitjob_runtime') THEN
    CREATE ROLE orbitjob_runtime LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orbitjob_operator') THEN
    CREATE ROLE orbitjob_operator LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOINHERIT;
  END IF;
END $$;

-- Object grants run in 0008 after ownership moves to orbitjob_table_owner.

-- Every tenant-owned table must enforce RLS. current_setting(..., true) returns
-- NULL when unset, so missing tenant context denies access instead of erroring.
ALTER TABLE jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_instances ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_instance_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE workers ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_change_audits ENABLE ROW LEVEL SECURITY;
ALTER TABLE checks ENABLE ROW LEVEL SECURITY;
ALTER TABLE check_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE slis ENABLE ROW LEVEL SECURITY;
ALTER TABLE slos ENABLE ROW LEVEL SECURITY;
ALTER TABLE sli_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE budgets ENABLE ROW LEVEL SECURITY;
ALTER TABLE budget_alerts ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;

DO $$
DECLARE
  table_name text;
  policy_name text;
BEGIN
  FOREACH table_name IN ARRAY ARRAY[
    'job_instance_attempts', 'workers', 'job_change_audits', 'checks',
    'check_runs', 'api_keys'
  ] LOOP
    policy_name := table_name || '_tenant_v020';
    EXECUTE format('DROP POLICY IF EXISTS %I ON %I', policy_name, table_name);
    EXECUTE format(
      'CREATE POLICY %I ON %I FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator USING (tenant_id = current_setting(''app.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''app.tenant_id'', true))',
      policy_name, table_name
    );
  END LOOP;
END $$;

-- Existing policies are PUBLIC; replace them with role-scoped policies.
DO $$
DECLARE
  table_name text;
  policy_name text;
BEGIN
  FOREACH table_name IN ARRAY ARRAY[
    'jobs', 'job_instances', 'audit_events', 'slis', 'slos',
    'sli_snapshots', 'budgets', 'budget_alerts'
  ] LOOP
    FOR policy_name IN SELECT policyname FROM pg_policies WHERE schemaname = 'public' AND tablename = table_name LOOP
      EXECUTE format('DROP POLICY IF EXISTS %I ON %I', policy_name, table_name);
    END LOOP;
    policy_name := table_name || '_tenant_v020';
    EXECUTE format(
      'CREATE POLICY %I ON %I FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator USING (tenant_id = current_setting(''app.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''app.tenant_id'', true))',
      policy_name, table_name
    );
  END LOOP;
END $$;

-- Tenant rows use id as the tenant boundary.
DROP POLICY IF EXISTS tenants_isolation ON tenants;
DROP POLICY IF EXISTS tenants_tenant_v020 ON tenants;
CREATE POLICY tenants_tenant_v020 ON tenants
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (id = current_setting('app.tenant_id', true))
  WITH CHECK (id = current_setting('app.tenant_id', true));

ALTER DEFAULT PRIVILEGES FOR ROLE orbitjob_table_owner IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO orbitjob_admin;

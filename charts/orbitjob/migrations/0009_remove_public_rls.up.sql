-- Remove legacy PUBLIC policies left on checks/check_runs before v0.2.0.
DROP POLICY IF EXISTS checks_tenant_isolation ON checks;
DROP POLICY IF EXISTS check_runs_tenant_isolation ON check_runs;

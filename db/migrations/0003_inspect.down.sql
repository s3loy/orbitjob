BEGIN;

DROP TRIGGER IF EXISTS trg_checks_set_updated_at ON checks;

DROP POLICY IF EXISTS checks_tenant_isolation ON checks;
DROP POLICY IF EXISTS check_runs_tenant_isolation ON check_runs;

DROP INDEX IF EXISTS idx_checks_active_next_run;
DROP INDEX IF EXISTS idx_checks_tenant_status;
DROP INDEX IF EXISTS idx_check_runs_pending_claim;
DROP INDEX IF EXISTS idx_check_runs_check_created;
DROP INDEX IF EXISTS uniq_check_runs_run_id;

DROP TABLE IF EXISTS check_runs;
DROP TABLE IF EXISTS checks;

COMMIT;

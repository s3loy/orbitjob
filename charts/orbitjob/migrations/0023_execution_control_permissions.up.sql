BEGIN;

-- Keep ownership explicit for direct migration runners as well as the normal
-- runner, which already assumes the non-login table owner before execution.
ALTER TABLE workflow_task_snapshots OWNER TO orbitjob_table_owner;
ALTER TABLE execution_source_controls OWNER TO orbitjob_table_owner;
ALTER FUNCTION guard_execution_dispatch() OWNER TO orbitjob_table_owner;
ALTER FUNCTION guard_workflow_snapshot_version() OWNER TO orbitjob_table_owner;
ALTER FUNCTION require_complete_workflow_snapshot() OWNER TO orbitjob_table_owner;
ALTER FUNCTION guard_workflow_task_snapshot() OWNER TO orbitjob_table_owner;
ALTER FUNCTION guard_workflow_snapshot_step() OWNER TO orbitjob_table_owner;
ALTER FUNCTION guard_source_control_identity() OWNER TO orbitjob_table_owner;
ALTER FUNCTION execution_tenant_active() OWNER TO orbitjob_table_owner;

-- Extend the named administrator preset only; custom policies stay unchanged.
UPDATE policies SET document=jsonb_set(document,'{statement,0,action}',(
  SELECT jsonb_agg(action ORDER BY action) FROM (
    SELECT DISTINCT jsonb_array_elements_text(document #> '{statement,0,action}') AS action
    UNION SELECT 'job:ManageExecution' UNION SELECT 'workflow:ManageExecution'
    UNION SELECT 'function:ManageExecution' UNION SELECT 'check:ManageExecution'
  ) actions))
WHERE id='00000000000000000000000011'
  AND name='TenantAdminAccess' AND tenant_id IS NULL;

COMMIT;

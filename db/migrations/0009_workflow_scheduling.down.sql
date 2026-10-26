BEGIN;
DROP INDEX IF EXISTS idx_workflow_open_identity;
DROP INDEX IF EXISTS ux_workflow_resource_uid;
ALTER TABLE workflow_run_control_plane DROP CONSTRAINT IF EXISTS workflow_scheduled_for_trigger;
ALTER TABLE workflow_run_control_plane DROP COLUMN IF EXISTS scheduled_for;
ALTER TABLE workflow_run_control_plane DROP CONSTRAINT IF EXISTS workflow_rejection_reason;
ALTER TABLE workflow_run_control_plane DROP COLUMN IF EXISTS rejection_reason;
ALTER TABLE workflow_run_control_plane DROP COLUMN IF EXISTS resource_uid;
COMMIT;

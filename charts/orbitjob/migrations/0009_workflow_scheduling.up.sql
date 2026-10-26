BEGIN;
-- Planned firing time is distinct from actual ledger acceptance. Historical
-- rows retain NULL because their intended instant cannot be reconstructed.
ALTER TABLE workflow_run_control_plane ADD COLUMN scheduled_for TIMESTAMPTZ,
  ADD COLUMN rejection_reason TEXT NOT NULL DEFAULT '',
  ADD COLUMN resource_uid TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_run_control_plane ADD CONSTRAINT workflow_rejection_reason
  CHECK ((phase = 'Rejected') = (rejection_reason <> ''));
ALTER TABLE workflow_run_control_plane ADD CONSTRAINT workflow_scheduled_for_trigger
  CHECK (scheduled_for IS NULL OR trigger = 'Schedule');
CREATE UNIQUE INDEX ux_workflow_resource_uid ON workflow_run_control_plane (resource_uid)
  WHERE resource_uid <> '';
CREATE INDEX idx_workflow_open_identity ON workflow_run_control_plane (tenant_id, source_uid)
  WHERE phase NOT IN ('Succeeded', 'Failed', 'Canceled', 'Rejected');
COMMIT;

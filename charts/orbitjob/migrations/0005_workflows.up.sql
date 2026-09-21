BEGIN;

-- ============================================================
-- Migration 0005: Workflows (DAG execution on the run ledger)
-- ============================================================
--
-- Design: docs/superpowers/plans/workflow-design.md, section 9.1, amended by
-- the approved rulings: the WorkflowJob CRD is ratified, manual workflow
-- triggering is IN SCOPE via a WorkflowRun CR (so the run row's trigger
-- vocabulary is Schedule and Manual, not Schedule alone), and retention is
-- rigorous — see the retainer notes at the bottom of this header.
--
-- One table, one column, the policies and grants that make both legal.
--
--   workflow_run_control_plane  the workflow run ledger: one row per
--                               workflow execution, deduplicated by
--                               occurrence, carrying the workflow-level
--                               phase and the skip-decision trail. The
--                               workflow row IS the workflow state.
--
--   job_run_control_plane.workflow_run_id   the step-grouping pointer: a
--                               step is an ordinary run of the referenced
--                               ScheduledJob definition (trigger 'Workflow')
--                               and this nullable column is the only thing
--                               that makes it a step.
--
-- Conventions, exactly the run-ledger family's: updated_at is maintained in
-- SQL by the writer (the ledger tables carry no set_updated_at trigger —
-- that is the configuration family's device), the actor is NOT NULL with a
-- non-empty CHECK because "who triggered this" is the product's claim, and
-- the phase column is free text VARCHAR(32) like its job-run sibling. There
-- is no CHECK on trigger: the column is VARCHAR(16) free text and both
-- 'Schedule' and 'Manual' fit, the same situation the Check value shipped
-- under.
--
-- Retention, per the approved ruling, stated here because the schema is its
-- substrate:
--
--   1. Task-definition retention EXCLUDES workflow steps: the retainer's
--      per-revision history sweep runs its predicate with
--      workflow_run_id IS NULL, so a definition's ordinary runs and its
--      workflow step runs are counted and pruned separately.
--   2. A dedicated atomic pass prunes workflow history steps-then-row in
--      ONE transaction: step rows delete first, the workflow row deletes
--      only after its last step is gone. The FK below is RESTRICT, so an
--      out-of-order delete fails loudly instead of orphaning steps.
--
-- Nothing auto-cascades and nothing lags silently: workflow rows can only be
-- pruned by the dedicated pass, and that pass cannot leave a step behind.
--
-- Migration numbering: the Workflows workstream carries 0005, immediately
-- after Functions' 0004. No preset-policy rows are touched here:
-- workflowjob:*/workflowrun:* preset actions land in the same change as the
-- routes that enforce them.
-- ============================================================

CREATE TABLE IF NOT EXISTS workflow_run_control_plane (
  id BIGSERIAL PRIMARY KEY,

  -- Tenant identifier for multi-tenant isolation. RESTRICT, matching
  -- job_run_control_plane: history is not deleted out from under a tenant.
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,

  -- The WorkflowJob CR's Kubernetes UID: the kubernetes-mode revision
  -- identity convention, the same discipline a ScheduledJob's revisions use.
  source_uid VARCHAR(128) NOT NULL,

  -- The workflow revision the run is pinned to. A step resolves the
  -- referenced definitions' active revisions at fire time; the workflow's
  -- own DAG comes from here.
  revision_id BIGINT NOT NULL REFERENCES job_definition_revisions(id),

  -- Deduplication key, derived from (source uid, revision, occurrence
  -- instant) for scheduled firings and from the caller's idempotency key
  -- material for manual ones.
  occurrence_key CHAR(64) NOT NULL,

  -- Why this run exists: 'Schedule' for a cron firing, 'Manual' for a run
  -- materialized from a WorkflowRun CR. Free text like the job-run column.
  trigger VARCHAR(16) NOT NULL,

  -- Who triggered the run. NOT NULL non-empty: a scheduled run has no human
  -- actor, and the writer states the walker's identity explicitly.
  actor VARCHAR(255) NOT NULL,

  -- Workflow-level lifecycle
  -- Pending | Running | CancelRequested | Succeeded | Failed | Canceled |
  -- CancelUnknown. A run with a CancelUnknown step stays CancelUnknown and
  -- non-terminal: a stop nobody could confirm is not an outcome.
  phase VARCHAR(32) NOT NULL,

  -- Skip-decision trail: task name -> {skipped, reason}. A skipped task has
  -- no job_run_control_plane row; this column is the only record of why.
  -- Reasons: canceled | fail_fast | condition | deadline.
  task_decisions JSONB NOT NULL DEFAULT '{}'::jsonb,

  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  -- One workflow run per schedule occurrence or manual trigger.
  UNIQUE (source_uid, occurrence_key),

  CONSTRAINT chk_workflow_run_actor_non_empty CHECK (btrim(actor) <> ''),

  CONSTRAINT chk_workflow_run_task_decisions_object
    CHECK (jsonb_typeof(task_decisions) = 'object'),

  CONSTRAINT chk_workflow_run_tenant_id_non_empty CHECK (tenant_id <> '')
);

CREATE INDEX IF NOT EXISTS idx_workflow_runs_tenant_phase
  ON workflow_run_control_plane (tenant_id, phase);

-- ------------------------------------------------------------
-- The step-grouping column. Nullable: only workflow steps carry it, and the
-- retainer's per-definition sweep reads the NULL side (workflow_run_id IS
-- NULL) to exclude steps from ordinary history. RESTRICT so a workflow row
-- cannot be dropped out from under its steps — the atomic prune deletes
-- steps first, in one transaction, and any other order fails here.
-- ------------------------------------------------------------

ALTER TABLE job_run_control_plane
  ADD COLUMN IF NOT EXISTS workflow_run_id BIGINT
    REFERENCES workflow_run_control_plane(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_job_run_workflow
  ON job_run_control_plane (workflow_run_id) WHERE workflow_run_id IS NOT NULL;

-- ------------------------------------------------------------
-- Row level security. The baseline's catalog assertion derives the protected
-- set from the tenant_id column: ENABLE and the policy land in this same
-- migration or the schema's own contract is broken.
-- ------------------------------------------------------------

ALTER TABLE workflow_run_control_plane ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS workflow_run_control_plane_tenant ON workflow_run_control_plane;
CREATE POLICY workflow_run_control_plane_tenant ON workflow_run_control_plane
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

-- ------------------------------------------------------------
-- Grants. The operator's runtime identity is the workflow ledger's writer
-- (the walker runs under the operator-singleton Lease); the admin API reads
-- and never writes — D1, and do not read the missing write grant as a bug
-- to fix; the operator role holds ledger parity with the other run tables.
--
-- No set_updated_at trigger exists to maintain: the writer sets updated_at
-- in its UPDATE statements, exactly as the job-run store does today.
-- ------------------------------------------------------------

GRANT SELECT, INSERT, UPDATE, DELETE ON workflow_run_control_plane TO orbitjob_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON workflow_run_control_plane TO orbitjob_operator;
GRANT SELECT ON workflow_run_control_plane TO orbitjob_admin;

-- The baseline's ON ALL SEQUENCES grant predates this sequence.
GRANT USAGE, SELECT ON SEQUENCE workflow_run_control_plane_id_seq
  TO orbitjob_runtime, orbitjob_operator;

COMMIT;

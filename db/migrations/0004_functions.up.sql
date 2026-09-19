BEGIN;

-- ============================================================
-- Migration 0004: Functions (serverless, HTTP-invoked one-shot runs)
-- ============================================================
--
-- Design: docs/superpowers/plans/serverless-design.md, section 4, with s3's
-- ruling that the function_runs derived read model is REQUIRED in v1 (the
-- design's open question 4, resolved the way checks resolved it: the
-- TerminalOutcomeRecorder hook upserts one row per terminal invocation).
--
-- Two tables, in the two families the schema already separates:
--
--   functions       configuration family. A tenant-owned definition row:
--                   image, command, args, timeout, retry budget, history,
--                   status, optional resource group. Materialized into
--                   job_definition_revisions per version under
--                   source_mode='function' (the checks pattern), so rendering
--                   needs zero new operator code and every invocation runs a
--                   pinned spec. tenant_id CASCADEs: configuration dies with
--                   the tenant.
--
--   function_runs   ledger-adjacent history (RESTRICT family, like
--                   check_runs and job_run_control_plane): a tenant with
--                   invocation history is not deleted out from under it. The
--                   derived read model for outcomes: one row per terminal
--                   invocation, written by the operator's terminal-phase
--                   bookkeeping, read by the admin API. The composite FK to
--                   functions(tenant_id, id) mirrors check_runs' identical
--                   check FK; soft delete keeps definition rows, so history
--                   never dangles it.
--
-- The ledger needs no DDL for function invocations:
-- job_run_control_plane.trigger is VARCHAR(16) with no CHECK, exactly the
-- situation the Check value shipped under.
--
-- Migration numbering: the Functions workstream carries 0004. No
-- preset-policy rows are touched here: an action earns a preset place only
-- when the HTTP API enforces it, so function:* preset actions land with the
-- routes that enforce them.
-- ============================================================

-- ------------------------------------------------------------
-- Table: functions
-- Family: configuration (tenant_id CASCADE; admin DML, runtime SELECT)
-- ------------------------------------------------------------

CREATE TABLE IF NOT EXISTS functions (

  -- Unique identifier of the function definition
  id BIGSERIAL PRIMARY KEY,

  -- Human readable function name
  name VARCHAR(128) NOT NULL,

  -- Optional description
  description TEXT,

  -- Tenant identifier for multi-tenant isolation
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

  -- Optional isolation tier this row belongs to. NULL means it belongs to no
  -- group, which the "*" group segment of an ARN matches.
  resource_group_id CHAR(26),

  -- Current function status
  -- active : the function may be invoked
  -- paused : invoke is refused with the suspended-definition conflict
  status VARCHAR(16) NOT NULL DEFAULT 'active',

  -- Container image the function runs. Required: an invocation renders a
  -- Kubernetes Job from the pinned revision, and an imageless Job is a
  -- failure discoverable only by watching pods.
  image VARCHAR(255) NOT NULL,

  -- Container command, an array of strings rendered into the Job's task
  -- container
  command JSONB NOT NULL DEFAULT '[]'::jsonb,

  -- Container args, an array of strings rendered into the Job's task
  -- container. v1 functions are parameterless: parameters are baked in here
  -- and changed by editing the definition, which is a new revision.
  args JSONB NOT NULL DEFAULT '[]'::jsonb,

  -- Maximum execution time allowed for one invocation, in seconds. Becomes
  -- the rendered Job's activeDeadlineSeconds, so enforcement survives an
  -- operator outage.
  timeout_seconds INT NOT NULL DEFAULT 60,

  -- Number of retries after the first failed attempt. The rendered retry
  -- budget is retry_limit + 1, matching the checks counting convention.
  retry_limit INT NOT NULL DEFAULT 0,

  -- Retention bounds for terminal invocations, per outcome. Defaults match
  -- the platform default (3/3) so retention sweeps function runs like every
  -- other run.
  history_success INT NOT NULL DEFAULT 3,
  history_failed INT NOT NULL DEFAULT 3,

  -- Function labels for filtering/grouping
  labels JSONB NOT NULL DEFAULT '{}'::jsonb,

  -- Optimistic concurrency control version number; also the revision
  -- generation the active pointer flips on
  version INT NOT NULL DEFAULT 1,

  -- Creation timestamp
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  -- Last modification timestamp
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  -- Soft delete timestamp
  deleted_at TIMESTAMPTZ,

  -- ------------------------------------------------------------
  -- Constraints
  -- ------------------------------------------------------------

  -- Validate function status
  CONSTRAINT chk_functions_status
    CHECK (status IN ('active', 'paused')),

  -- An imageless function cannot render
  CONSTRAINT chk_functions_image_non_empty CHECK (btrim(image) <> ''),

  -- Command and args are arrays of strings
  CONSTRAINT chk_functions_command_array CHECK (jsonb_typeof(command) = 'array'),
  CONSTRAINT chk_functions_args_array CHECK (jsonb_typeof(args) = 'array'),

  -- Labels are an object
  CONSTRAINT chk_functions_labels_object CHECK (jsonb_typeof(labels) = 'object'),

  -- Ensure numeric fields are valid. timeout_seconds has a floor of 1 (a
  -- zero-second Job is a zero-second failure); the ceiling is boundary
  -- validation's to enforce so a definition saved before a tighter cap can
  -- still be read honestly.
  CONSTRAINT chk_functions_non_negative CHECK (
    timeout_seconds >= 1 AND
    retry_limit >= 0 AND
    history_success >= 0 AND
    history_failed >= 0 AND
    version >= 1
  ),

  CONSTRAINT chk_functions_tenant_id_non_empty CHECK (tenant_id <> ''),

  -- Support tenant-scoped composite foreign key reference from the read model
  CONSTRAINT uq_functions_tenant_id_id UNIQUE (tenant_id, id)
);

-- Function listing index
CREATE INDEX IF NOT EXISTS idx_functions_tenant_status
ON functions(tenant_id, status)
WHERE deleted_at IS NULL;

-- ------------------------------------------------------------
-- Table: function_runs
-- Family: ledger-adjacent history (tenant_id RESTRICT; the operator writes,
-- the admin reads). Derived read model, mirroring check_runs structurally.
-- ------------------------------------------------------------

CREATE TABLE IF NOT EXISTS function_runs (

  -- Internal run identifier
  id BIGSERIAL PRIMARY KEY,

  -- Deterministic run identifier derived from the ledger occurrence key, so
  -- a replayed recording addresses the same read-model row
  run_id UUID NOT NULL DEFAULT gen_random_uuid(),

  -- Tenant identifier copied from the run for tenant-level isolation.
  -- RESTRICT: invocation history is not deleted out from under a tenant.
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,

  -- Associated function definition. The composite foreign key below pins
  -- the run's tenant to the definition's, mirroring check_runs; soft delete
  -- keeps definition rows, so history never dangles it.
  function_id BIGINT NOT NULL,

  -- Terminal outcome of the invocation
  -- success  : the run reached Succeeded
  -- failed   : the run reached Failed
  -- canceled : the run reached Canceled
  -- There is no pending or running state: live progress lives on the JobRun
  -- CR and the ledger row, not in this table.
  status VARCHAR(16) NOT NULL,

  -- When the invocation was created (the ledger row's created_at). A
  -- function invocation has no scheduled instant.
  triggered_at TIMESTAMPTZ NOT NULL,

  -- Actual execution start time
  started_at TIMESTAMPTZ,

  -- Execution finish time
  finished_at TIMESTAMPTZ,

  -- Execution duration in milliseconds
  duration_ms INT,

  -- Optimistic locking version
  version INT NOT NULL DEFAULT 1,

  -- Creation timestamp
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  -- ------------------------------------------------------------
  -- Constraints
  -- ------------------------------------------------------------

  -- Validate read-model status
  CONSTRAINT chk_function_runs_status
    CHECK (status IN ('success', 'failed', 'canceled')),

  -- Ensure timestamp fields are consistent with status. An exit outcome
  -- started and finished; a canceled invocation may have been stopped
  -- before its first attempt began, so started_at stays nullable there.
  CONSTRAINT chk_function_runs_status_timestamps CHECK (
    (
      status IN ('success', 'failed')
      AND started_at IS NOT NULL
      AND finished_at IS NOT NULL
      AND finished_at >= started_at
    ) OR (
      status = 'canceled'
      AND finished_at IS NOT NULL
      AND (started_at IS NULL OR finished_at >= started_at)
    )
  ),

  -- Ensure duration is valid when present
  CONSTRAINT chk_function_runs_duration CHECK (
    duration_ms IS NULL OR duration_ms >= 0
  ),

  -- Support tenant-scoped composite foreign key reference
  CONSTRAINT uq_function_runs_tenant_id_id UNIQUE (tenant_id, id),

  -- Ensure run tenant matches parent function tenant
  CONSTRAINT fk_function_runs_function_tenant
    FOREIGN KEY (tenant_id, function_id) REFERENCES functions(tenant_id, id),

  CONSTRAINT chk_function_runs_tenant_id_non_empty CHECK (tenant_id <> '')
);

-- Function invocation history index
CREATE INDEX IF NOT EXISTS idx_function_runs_function_created
ON function_runs(tenant_id, function_id, created_at DESC);

-- Unique run identifier index (the upsert anchor)
CREATE UNIQUE INDEX IF NOT EXISTS uniq_function_runs_run_id
ON function_runs(run_id);

-- ------------------------------------------------------------
-- Row level security. The structural catalog assertion derives the protected
-- set from the tenant_id column: a tenant-owned table without RLS fails the
-- migration outright. Policy and ENABLE land in this same file.
-- ------------------------------------------------------------

ALTER TABLE functions ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS functions_tenant ON functions;
CREATE POLICY functions_tenant ON functions
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE function_runs ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS function_runs_tenant ON function_runs;
CREATE POLICY function_runs_tenant ON function_runs
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

-- ------------------------------------------------------------
-- Grants.
--
-- functions is configuration: the admin API owns it end to end. The runtime
-- role (the operator's revision-sync loop) reads active definitions and
-- writes nothing — the tenants SELECT precedent. The operator role stays
-- ledger-only.
--
-- function_runs is derived history under the D1 rule: the operator's
-- terminal-phase bookkeeping is the one writer, the admin API reads. The
-- admin's SELECT-only grant here is deliberate and differs from
-- check_runs' admin DML, which is a legacy of the pre-0002 work-queue era
-- this table is born without.
-- ------------------------------------------------------------

GRANT SELECT, INSERT, UPDATE, DELETE ON functions TO orbitjob_admin;
GRANT SELECT ON functions TO orbitjob_runtime;

GRANT SELECT, INSERT, UPDATE, DELETE ON function_runs TO orbitjob_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON function_runs TO orbitjob_operator;
GRANT SELECT ON function_runs TO orbitjob_admin;

-- The baseline's ON ALL SEQUENCES grant predates these sequences.
GRANT USAGE, SELECT ON SEQUENCE functions_id_seq TO orbitjob_admin;
GRANT USAGE, SELECT ON SEQUENCE function_runs_id_seq TO orbitjob_runtime, orbitjob_operator;

COMMIT;

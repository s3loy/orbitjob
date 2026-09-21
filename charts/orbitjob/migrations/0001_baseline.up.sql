BEGIN;

-- ============================================================
-- OrbitJob database baseline
-- ============================================================
--
-- One file, applied once to an empty database. It replaces the previous
-- 0001_baseline plus migrations 0002-0005. The project is unreleased (no git
-- tag) with a single development database, so nothing had to be preserved and
-- the schema can describe the product that is actually being built -- a
-- Kubernetes-only run ledger -- instead of the legacy scheduler path plus
-- patches. See docs/prd.md section 3 and decisions D1/D2 at the top of
-- docs/superpowers/workstreams/PLAN.md.
--
-- Not created, on purpose:
--   jobs, job_instances, job_instance_attempts
--     the legacy scheduler -> dispatcher -> worker path. The run ledger is
--     job_definition_revisions + job_run_control_plane +
--     job_run_attempts_control_plane. No production Go references jobs.
--   control_plane_tenant_mode
--     the per-tenant execution mode is gone. One path remains, so there is no
--     mode to switch and no writer_epoch to fence with.
--   workers
--     legacy worker registration; there are no workers to register.
--   scheduler_leaders
--     zero readers and zero writers in production Go.
--   job_change_audits
--     zero readers and zero writers in production Go. audit_events carries the
--     change trail instead (ADR 0005).
--
-- Tenant isolation contract, carried over unchanged:
--   a tenant-owned table has a tenant_id column and a row level security
--   policy bound to current_setting('app.tenant_id', true). Writers set that
--   GUC inside the transaction (postgres.WithTenantTransaction).
--   RLS is ENABLE-only, never FORCE (ADR 0001): the table owner is a NOLOGIN
--   role, so the owner-bypass path FORCE would guard against is unreachable.
--
-- The structural catalog assertion near the end of this file enforces that
-- contract from the catalog rather than from a list of table names.

-- ============================================================
-- Required roles
-- ------------------------------------------------------------
-- The baseline never creates cluster-level roles; owner-init does. It checks
-- here, before creating anything, so a missing role fails fast instead of
-- leaving a half-built schema behind.
-- ============================================================
DO $$
DECLARE
  required_role text;
BEGIN
  FOREACH required_role IN ARRAY ARRAY[
    'orbitjob_table_owner', 'orbitjob_migrator', 'orbitjob_admin',
    'orbitjob_runtime', 'orbitjob_operator', 'orbitjob_owner',
    'orbitjob_reader'
  ] LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = required_role) THEN
      RAISE EXCEPTION 'required database role % is missing; run owner-init first', required_role;
    END IF;
  END LOOP;

  IF NOT pg_has_role('orbitjob_migrator', 'orbitjob_table_owner', 'MEMBER') THEN
    RAISE EXCEPTION 'required role membership orbitjob_migrator -> orbitjob_table_owner is missing';
  END IF;
END
$$;

-- ============================================================
-- Table: tenants
-- ============================================================
CREATE TABLE IF NOT EXISTS tenants (
  id CHAR(26) PRIMARY KEY,
  slug VARCHAR(64) NOT NULL,
  name VARCHAR(128) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'active',
  quotas JSONB NOT NULL DEFAULT '{}',
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT uniq_tenants_slug UNIQUE (slug),
  CONSTRAINT chk_tenants_status CHECK (status IN ('active', 'suspended')),
  CONSTRAINT chk_tenants_slug_non_empty CHECK (slug <> ''),
  CONSTRAINT chk_tenants_name_non_empty CHECK (name <> '')
);

-- ============================================================
-- Table: resource_groups
-- Description:
--   Optional isolation tier inside a tenant, playing the role a
--   Kubernetes namespace plays. A resource with a NULL group belongs to
--   no group and is matched by the "*" group segment of an ARN.
-- ============================================================
CREATE TABLE IF NOT EXISTS resource_groups (
  id CHAR(26) PRIMARY KEY,
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  slug VARCHAR(64) NOT NULL,
  name VARCHAR(128) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT uniq_resource_groups_slug UNIQUE (tenant_id, slug),
  CONSTRAINT chk_resource_groups_slug_non_empty CHECK (slug <> '')
);

CREATE INDEX IF NOT EXISTS idx_resource_groups_tenant ON resource_groups(tenant_id);

-- ============================================================
-- Table: policies
-- Description:
--   A named, reusable authorization document. tenant_id IS NULL marks a
--   platform preset that every tenant may bind but none may edit.
-- ============================================================
CREATE TABLE IF NOT EXISTS policies (
  id CHAR(26) PRIMARY KEY,
  tenant_id CHAR(26) REFERENCES tenants(id) ON DELETE CASCADE,
  name VARCHAR(128) NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  document JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT chk_policies_name_non_empty CHECK (name <> ''),
  CONSTRAINT chk_policies_document_object CHECK (jsonb_typeof(document) = 'object')
);

CREATE UNIQUE INDEX IF NOT EXISTS uniq_policies_tenant_name
  ON policies(tenant_id, name) WHERE tenant_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uniq_policies_platform_name
  ON policies(name) WHERE tenant_id IS NULL;

-- ============================================================
-- Table: api_keys
-- Description:
--   Credentials. kind separates a platform principal (tenant_id IS NULL,
--   operates across tenants) from a tenant principal. The policy documents
--   bound through key_policies are the grants; boundary_policy_id caps them.
-- ============================================================
CREATE TABLE IF NOT EXISTS api_keys (
  id CHAR(26) PRIMARY KEY,
  tenant_id CHAR(26) REFERENCES tenants(id),
  kind TEXT NOT NULL DEFAULT 'tenant',
  resource_group_id CHAR(26) REFERENCES resource_groups(id),
  boundary_policy_id CHAR(26) REFERENCES policies(id),
  key_hash VARCHAR(60) NOT NULL,
  key_prefix VARCHAR(12) NOT NULL,
  expires_at TIMESTAMPTZ,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_by VARCHAR(64),
  CONSTRAINT chk_api_keys_key_hash_non_empty CHECK (key_hash <> ''),
  CONSTRAINT chk_api_keys_key_prefix_non_empty CHECK (key_prefix <> ''),
  CONSTRAINT chk_api_keys_kind CHECK (kind IN ('platform', 'tenant')),
  -- A platform principal has no tenant; a tenant principal must have one.
  -- Making the inconsistent row unrepresentable beats validating it in code.
  CONSTRAINT chk_api_keys_kind_tenant CHECK (
    (kind = 'platform' AND tenant_id IS NULL) OR
    (kind = 'tenant'   AND tenant_id IS NOT NULL)
  )
);

CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys(tenant_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(key_prefix);

-- ============================================================
-- Table: key_policies
-- Description:
--   Binds policies to keys. A key's grants are the union of its bound
--   policies, narrowed by its boundary. bound_by records who granted it,
--   which is the audit trail for "who can create bindings" -- itself a
--   privilege-escalation vector worth being able to answer after the fact.
-- ============================================================
CREATE TABLE IF NOT EXISTS key_policies (
  key_id CHAR(26) NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  policy_id CHAR(26) NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
  bound_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  bound_by VARCHAR(64),
  PRIMARY KEY (key_id, policy_id)
);

CREATE INDEX IF NOT EXISTS idx_key_policies_policy ON key_policies(policy_id);

-- ============================================================
-- Table: audit_events (partitioned by created_at, INSERT-only)
-- Description:
--   One trail for every privileged change. Audit rows commit in the same
--   transaction as the change they describe (ADR 0005).
--
--   History family (audit_events, job_definition_revisions,
--   job_run_control_plane, job_run_attempts_control_plane, check_runs,
--   sli_snapshots, budget_alerts): tenant_id RESTRICTs -- a tenant with
--   history cannot be deleted until the history is explicitly purged. That
--   explicitness is the point for an audit product.
-- ============================================================
CREATE TABLE IF NOT EXISTS audit_events (
  id BIGSERIAL,
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  actor_type VARCHAR(32) NOT NULL,
  actor_id VARCHAR(64),
  event_type VARCHAR(32) NOT NULL,
  resource_type VARCHAR(32) NOT NULL,
  resource_id VARCHAR(64) NOT NULL,
  diff JSONB NOT NULL DEFAULT '{}',
  trace_id VARCHAR(64),
  idempotency_key VARCHAR(128),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT chk_audit_actor_type CHECK (actor_type IN ('system', 'api_key', 'user')),
  CONSTRAINT chk_audit_tenant_non_empty CHECK (tenant_id <> '')
) PARTITION BY RANGE (created_at);

-- Default partition: catches all rows until monthly partitions are created.
-- Once specific partitions exist (e.g. FOR VALUES FROM ... TO ...), this
-- partition only receives rows that don't match any explicit partition.
CREATE TABLE IF NOT EXISTS audit_events_default PARTITION OF audit_events DEFAULT;

CREATE INDEX IF NOT EXISTS idx_audit_events_tenant_time
  ON audit_events(tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_audit_events_resource
  ON audit_events(tenant_id, resource_type, resource_id, created_at DESC);

-- ============================================================
-- Run ledger (Kubernetes control plane)
-- ------------------------------------------------------------
-- Authority split enforced here:
--   job_definition_revisions        immutable definition revisions + active pointer
--   job_run_control_plane           one row per logical run, deduplicated by occurrence
--   job_run_attempts_control_plane  one row per platform attempt == one Kubernetes Job
--
-- This is the only run path; the legacy jobs/job_instances tables are gone.
-- ============================================================

CREATE TABLE IF NOT EXISTS job_definition_revisions (
  id BIGSERIAL PRIMARY KEY,
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  source_mode VARCHAR(16) NOT NULL,
  source_uid VARCHAR(128) NOT NULL,
  source_namespace VARCHAR(255) NOT NULL,
  source_name VARCHAR(255) NOT NULL,
  generation BIGINT NOT NULL CHECK (generation > 0),
  spec_hash CHAR(64) NOT NULL,
  normalized_spec JSONB NOT NULL,
  -- Active pointer. Exactly one revision per definition identity may be active;
  -- the scheduler only reads the active row. Switched in the same transaction
  -- that inserts the revision, so no reader observes a spec without its revision.
  is_active BOOLEAN NOT NULL DEFAULT false,
  -- Who projected this revision. NOT NULL with no default: a revision whose
  -- author cannot be named is the same gap as a run whose triggerer cannot be
  -- named, and the product exists to answer both. A DEFAULT would let an
  -- omitted actor be stored as an empty string without anything failing.
  actor VARCHAR(255) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (source_mode, source_uid, generation),
  CONSTRAINT chk_job_definition_revisions_actor_non_empty CHECK (btrim(actor) <> '')
);

-- At most one active revision per identity.
CREATE UNIQUE INDEX IF NOT EXISTS ux_job_definition_revisions_active
  ON job_definition_revisions (source_mode, source_uid)
  WHERE is_active;

CREATE INDEX IF NOT EXISTS idx_job_definition_revisions_tenant
  ON job_definition_revisions (tenant_id, source_uid);

CREATE TABLE IF NOT EXISTS job_run_control_plane (
  id BIGSERIAL PRIMARY KEY,
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  source_uid VARCHAR(128) NOT NULL,
  revision_id BIGINT NOT NULL REFERENCES job_definition_revisions(id),
  occurrence_key CHAR(64) NOT NULL,
  trigger VARCHAR(16) NOT NULL,
  -- Who triggered the run. The ledger's claim is "did it run, how many times,
  -- and who triggered it"; without this column the answer cannot be recovered
  -- after the fact. It rides on the JobRun resource and the operator writes it
  -- in the run's transaction (PLAN.md D1).
  --
  -- NOT NULL and non-empty on purpose. A nullable actor would let a writer omit
  -- it and produce exactly the row the claim cannot answer; a DEFAULT sentinel
  -- would hide a forgotten write behind a plausible value. VARCHAR(255) matches
  -- the schema's other actor columns (job_definition_revisions.actor,
  -- audit_events.actor_id is narrower because it holds a key or user id; a run
  -- actor can be a full Kubernetes identity). A scheduled run has no human
  -- actor; the writer states the scheduler identity explicitly.
  actor VARCHAR(255) NOT NULL,
  phase VARCHAR(32) NOT NULL,
  attempt INT NOT NULL DEFAULT 0 CHECK (attempt >= 0),
  max_attempts INT NOT NULL DEFAULT 1 CHECK (max_attempts >= 1),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- One logical run per schedule occurrence or manual trigger.
  UNIQUE (source_uid, occurrence_key),
  CONSTRAINT chk_job_run_control_plane_actor_non_empty CHECK (btrim(actor) <> '')
);

CREATE INDEX IF NOT EXISTS idx_job_run_control_plane_tenant_phase
  ON job_run_control_plane (tenant_id, phase);

CREATE TABLE IF NOT EXISTS job_run_attempts_control_plane (
  id BIGSERIAL PRIMARY KEY,
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  run_id BIGINT NOT NULL REFERENCES job_run_control_plane(id) ON DELETE CASCADE,
  attempt_number INT NOT NULL CHECK (attempt_number > 0),
  phase VARCHAR(32) NOT NULL,
  kubernetes_job_name VARCHAR(63) NOT NULL,
  -- Observed Kubernetes identity. Name alone is not ownership proof: a deleted
  -- and recreated Job keeps its name but gets a new UID.
  kubernetes_job_uid VARCHAR(128),
  observed_resource_version VARCHAR(128),
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (run_id, attempt_number),
  UNIQUE (kubernetes_job_name)
);

CREATE INDEX IF NOT EXISTS idx_job_run_attempts_tenant_phase
  ON job_run_attempts_control_plane (tenant_id, phase);

-- ============================================================
-- Table: checks
-- Description:
--   Stores inspection/check definitions managed by the inspection
--   scheduler. A check represents a health/metric/SSL inspection
--   that produces execution records in check_runs.
--
--   Configuration family (checks, slis, slos, budgets): tenant_id cascades --
--   a tenant's configuration dies with the tenant.
-- ============================================================
CREATE TABLE IF NOT EXISTS checks (

  -- Unique identifier of the check definition
  id BIGSERIAL PRIMARY KEY,

  -- Human readable check name
  name VARCHAR(128) NOT NULL,

  -- Optional description
  description TEXT,

  -- Tenant identifier for multi-tenant isolation
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

  -- Optional isolation tier this row belongs to. NULL means it belongs to no
  -- group, which the "*" group segment of an ARN matches.
  resource_group_id CHAR(26),

  -- Current check status
  -- active : scheduler will generate runs
  -- paused : scheduler will ignore check
  status VARCHAR(16) NOT NULL DEFAULT 'active',

  -- Type of inspection
  -- http_health : HTTP endpoint health check
  check_type VARCHAR(32) NOT NULL,

  -- Check-specific configuration payload
  -- e.g. {"url":"http://api/health","method":"GET","expected_status":200}
  check_config JSONB NOT NULL DEFAULT '{}'::jsonb,

  -- Assertion rules for evaluation engine
  -- e.g. [{"metric":"response_time_ms","operator":">","threshold":1000,"severity":"warning"}]
  assertion_rules JSONB NOT NULL DEFAULT '[]'::jsonb,

  -- Type of scheduling mechanism
  -- cron     : scheduled execution using cron expression
  -- interval : fixed interval in seconds
  schedule_type VARCHAR(16) NOT NULL DEFAULT 'cron',

  -- Cron expression defining execution schedule
  -- Required when schedule_type = 'cron'
  cron_expr VARCHAR(128),

  -- Fixed interval in seconds
  -- Required when schedule_type = 'interval'
  interval_sec INT,

  -- Timezone used to evaluate schedule
  timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',

  -- Maximum execution time allowed for a check run
  timeout_sec INT NOT NULL DEFAULT 30,

  -- Maximum retry attempts after failure
  retry_limit INT NOT NULL DEFAULT 2,

  -- Scheduling priority (higher value means higher priority)
  priority INT NOT NULL DEFAULT 5,

  -- Check labels for filtering/grouping
  labels JSONB NOT NULL DEFAULT '{}'::jsonb,

  -- Next calculated execution time
  next_run_at TIMESTAMPTZ,

  -- Optimistic concurrency control version number
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

  -- Validate check status
  CONSTRAINT chk_checks_status
    CHECK (status IN ('active', 'paused')),

  -- Validate schedule type
  CONSTRAINT chk_checks_schedule_type
    CHECK (schedule_type IN ('cron', 'interval')),

  -- Validate check type
  CONSTRAINT chk_checks_check_type
    CHECK (check_type IN ('http_health')),

  -- Enforce cron_expr when using cron schedule
  CONSTRAINT chk_checks_cron_expr_required CHECK (
    (
      schedule_type = 'cron'
      AND cron_expr IS NOT NULL
      AND btrim(cron_expr) <> ''
    ) OR (
      schedule_type = 'interval'
      AND cron_expr IS NULL
    )
  ),

  -- Enforce interval_sec when using interval schedule
  CONSTRAINT chk_checks_interval_sec_required CHECK (
    (
      schedule_type = 'interval'
      AND interval_sec IS NOT NULL
      AND interval_sec >= 1
    ) OR (
      schedule_type = 'cron'
      AND interval_sec IS NULL
    )
  ),

  -- Ensure numeric fields are valid
  CONSTRAINT chk_checks_non_negative CHECK (
    priority >= 0 AND
    timeout_sec >= 1 AND
    retry_limit >= 0 AND
    version >= 1
  ),

  CONSTRAINT chk_checks_tenant_id_non_empty CHECK (tenant_id <> ''),

  CONSTRAINT chk_checks_check_config_object CHECK (jsonb_typeof(check_config) = 'object'),

  CONSTRAINT chk_checks_assertion_rules_array CHECK (jsonb_typeof(assertion_rules) = 'array'),

  CONSTRAINT chk_checks_labels_object CHECK (jsonb_typeof(labels) = 'object'),

  -- Support tenant-scoped composite foreign key reference from runs
  CONSTRAINT uq_checks_tenant_id_id UNIQUE (tenant_id, id)
);

-- ============================================================
-- Table: check_runs
-- Description:
--   Stores execution records (runs) generated from checks.
--   Each scheduled execution corresponds to one row.
-- ============================================================
CREATE TABLE IF NOT EXISTS check_runs (

  -- Internal run identifier
  id BIGSERIAL PRIMARY KEY,

  -- Unique execution run identifier
  run_id UUID NOT NULL DEFAULT gen_random_uuid(),

  -- Tenant identifier copied from checks for tenant-level isolation
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,

  -- Associated check definition
  check_id BIGINT NOT NULL,

  -- Current lifecycle state of run
  status VARCHAR(16) NOT NULL DEFAULT 'pending',

  -- Severity level determined by evaluation engine
  -- ok       : all assertions passed
  -- warning  : at least one warning assertion triggered
  -- critical : at least one critical assertion triggered
  -- unknown  : evaluation could not be performed
  severity VARCHAR(16),

  -- Raw check output data
  output JSONB NOT NULL DEFAULT '{}'::jsonb,

  -- Evaluation result from assertion rules
  evaluation_result JSONB NOT NULL DEFAULT '{}'::jsonb,

  -- Scheduled execution time
  scheduled_at TIMESTAMPTZ NOT NULL,

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

  -- Validate run status
  CONSTRAINT chk_check_runs_status
    CHECK (status IN ('pending', 'running', 'success', 'failed')),

  -- Validate severity
  CONSTRAINT chk_check_runs_severity
    CHECK (severity IS NULL OR severity IN ('ok', 'warning', 'critical', 'unknown')),

  -- Ensure timestamp fields are consistent with status
  CONSTRAINT chk_check_runs_status_timestamps CHECK (
    (
      status = 'pending'
      AND started_at IS NULL
      AND finished_at IS NULL
    ) OR (
      status = 'running'
      AND started_at IS NOT NULL
      AND finished_at IS NULL
    ) OR (
      status IN ('success', 'failed')
      AND started_at IS NOT NULL
      AND finished_at IS NOT NULL
      AND finished_at >= started_at
    )
  ),

  -- Ensure duration is valid when present
  CONSTRAINT chk_check_runs_duration CHECK (
    duration_ms IS NULL OR duration_ms >= 0
  ),

  -- Support tenant-scoped composite foreign key reference
  CONSTRAINT uq_check_runs_tenant_id_id UNIQUE (tenant_id, id),

  -- Ensure run tenant matches parent check tenant
  CONSTRAINT fk_check_runs_check_tenant
    FOREIGN KEY (tenant_id, check_id) REFERENCES checks(tenant_id, id),

  CONSTRAINT chk_check_runs_tenant_id_non_empty CHECK (tenant_id <> '')
);

-- ============================================================
-- Indexes for checks and check_runs
-- ============================================================

-- Scheduler scanning index for checks
CREATE INDEX IF NOT EXISTS idx_checks_active_next_run
ON checks(tenant_id, status, next_run_at)
WHERE deleted_at IS NULL AND next_run_at IS NOT NULL;

-- Check listing index
CREATE INDEX IF NOT EXISTS idx_checks_tenant_status
ON checks(tenant_id, status)
WHERE deleted_at IS NULL;

-- Check run claim index
CREATE INDEX IF NOT EXISTS idx_check_runs_pending_claim
ON check_runs(tenant_id, check_id, scheduled_at)
WHERE status = 'pending';

-- Check run history index
CREATE INDEX IF NOT EXISTS idx_check_runs_check_created
ON check_runs(tenant_id, check_id, created_at DESC);

-- Unique run identifier index
CREATE UNIQUE INDEX IF NOT EXISTS uniq_check_runs_run_id
ON check_runs(run_id);

-- ============================================================
-- SLO/SLI system
--   SLI definitions, SLO targets, pre-aggregated snapshots, error budget
--   tracking, and burn rate alerts.
-- ============================================================

CREATE TABLE IF NOT EXISTS slis (
    id BIGSERIAL PRIMARY KEY,
    tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    -- Optional isolation tier this row belongs to. NULL means it belongs to no
    -- group, which the "*" group segment of an ARN matches.
    resource_group_id CHAR(26),
    name VARCHAR(128) NOT NULL,
    description TEXT,
    sli_type VARCHAR(32) NOT NULL,
    source_type VARCHAR(32) NOT NULL DEFAULT 'check_run',
    source_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    aggregation VARCHAR(32) NOT NULL DEFAULT 'ratio',
    good_event_criteria JSONB NOT NULL DEFAULT '{}'::jsonb,
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_slis_tenant ON slis(tenant_id);
CREATE INDEX IF NOT EXISTS idx_slis_tenant_deleted ON slis(tenant_id, deleted_at) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS slos (
    id BIGSERIAL PRIMARY KEY,
    tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    -- Optional isolation tier this row belongs to. NULL means it belongs to no
    -- group, which the "*" group segment of an ARN matches.
    resource_group_id CHAR(26),
    name VARCHAR(128) NOT NULL,
    description TEXT,
    sli_id BIGINT NOT NULL REFERENCES slis(id),
    target DECIMAL(5,4) NOT NULL,
    window_type VARCHAR(16) NOT NULL DEFAULT 'rolling',
    window_duration BIGINT NOT NULL DEFAULT 2592000,
    alert_fast_burn_rate DECIMAL(6,2) NOT NULL DEFAULT 14.4,
    alert_slow_burn_rate DECIMAL(6,2) NOT NULL DEFAULT 2.0,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_slos_tenant ON slos(tenant_id);
CREATE INDEX IF NOT EXISTS idx_slos_tenant_status ON slos(tenant_id, status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_slos_sli ON slos(sli_id);

CREATE TABLE IF NOT EXISTS sli_snapshots (
    id BIGSERIAL PRIMARY KEY,
    tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    sli_id BIGINT NOT NULL REFERENCES slis(id),
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    good_events_count BIGINT NOT NULL DEFAULT 0,
    total_events_count BIGINT NOT NULL DEFAULT 0,
    sli_value DECIMAL(10,6),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, sli_id, window_start)
);

CREATE INDEX IF NOT EXISTS idx_sli_snapshots_lookup ON sli_snapshots(tenant_id, sli_id, window_start, window_end);
CREATE INDEX IF NOT EXISTS idx_sli_snapshots_window ON sli_snapshots(window_start, window_end);

CREATE TABLE IF NOT EXISTS budgets (
    id BIGSERIAL PRIMARY KEY,
    tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    slo_id BIGINT NOT NULL REFERENCES slos(id),
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    budget_total DECIMAL(20,10) NOT NULL,
    budget_consumed DECIMAL(20,10) NOT NULL DEFAULT 0,
    budget_remaining DECIMAL(20,10) NOT NULL,
    burn_rate DECIMAL(10,4),
    status VARCHAR(16) NOT NULL DEFAULT 'healthy',
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, slo_id, window_start)
);

CREATE INDEX IF NOT EXISTS idx_budgets_lookup ON budgets(tenant_id, slo_id, window_start, window_end);
CREATE INDEX IF NOT EXISTS idx_budgets_status ON budgets(tenant_id, status) WHERE status IN ('at_risk', 'exhausted');

CREATE TABLE IF NOT EXISTS budget_alerts (
    id BIGSERIAL PRIMARY KEY,
    tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    slo_id BIGINT NOT NULL REFERENCES slos(id),
    budget_id BIGINT NOT NULL REFERENCES budgets(id),
    alert_type VARCHAR(16) NOT NULL,
    burn_rate DECIMAL(10,4) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_budget_alerts_lookup ON budget_alerts(tenant_id, slo_id, status);
CREATE INDEX IF NOT EXISTS idx_budget_alerts_active ON budget_alerts(tenant_id, status) WHERE status = 'active';

-- ============================================================
-- Functions
-- ============================================================

-- Trigger function: keeps updated_at current on row modification.
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Narrow cross-tenant entry points required before a tenant context is known.
-- SECURITY DEFINER so they run as the table owner and can see rows the caller's
-- RLS policy would hide; each is restricted to exactly the role that needs it.
CREATE OR REPLACE FUNCTION orbitjob_auth_api_key(p_prefix text)
RETURNS TABLE(id text, tenant_id text, kind text, resource_group_id text,
              boundary_policy_id text, key_hash text, revoked text, expired text)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
  SELECT ak.id::text, ak.tenant_id::text, ak.kind, ak.resource_group_id::text,
         ak.boundary_policy_id::text, ak.key_hash::text,
         CASE WHEN ak.revoked_at IS NOT NULL THEN 'revoked' END,
         CASE WHEN ak.expires_at IS NOT NULL AND ak.expires_at < now() THEN 'expired' END
  FROM public.api_keys ak
  WHERE ak.key_prefix = p_prefix
    AND ak.revoked_at IS NULL
    AND (ak.tenant_id IS NULL OR EXISTS (
      SELECT 1 FROM public.tenants t WHERE t.id = ak.tenant_id AND t.status = 'active'
    ))
  ORDER BY ak.created_at DESC
$$;

CREATE OR REPLACE FUNCTION orbitjob_list_active_tenant_ids()
RETURNS TABLE(id text)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
  SELECT t.id::text FROM public.tenants t WHERE t.status = 'active' ORDER BY t.id
$$;

-- Bootstrap is the only cross-tenant write path. The caller supplies a bcrypt
-- hash; plaintext API keys never enter PostgreSQL functions or logs.
CREATE OR REPLACE FUNCTION orbitjob_bootstrap_default(
  p_tenant_id text,
  p_tenant_slug text,
  p_tenant_name text,
  p_tenant_status text,
  p_key_id text,
  p_key_hash text,
  p_key_prefix text
)
RETURNS TABLE(tenant_created boolean, key_created boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
  tenant_rows bigint;
  key_rows bigint;
BEGIN
  IF session_user <> 'orbitjob_admin' THEN
    RAISE EXCEPTION 'bootstrap function requires orbitjob_admin';
  END IF;
  IF p_tenant_id <> '00000000000000000000000001'
     OR p_tenant_slug <> 'default'
     OR p_tenant_name <> 'Default'
     OR p_tenant_status <> 'active'
     OR p_key_id <> '00000000000000000000000002' THEN
    RAISE EXCEPTION 'bootstrap function only accepts the default bootstrap identity';
  END IF;

  INSERT INTO public.tenants (id, slug, name, status, created_at, updated_at)
  VALUES (p_tenant_id, p_tenant_slug, p_tenant_name, p_tenant_status, now(), now())
  ON CONFLICT (id) DO NOTHING;
  GET DIAGNOSTICS tenant_rows = ROW_COUNT;

  IF tenant_rows = 0 AND NOT EXISTS (
    SELECT 1 FROM public.tenants
    WHERE id = p_tenant_id
      AND slug = p_tenant_slug
      AND name = p_tenant_name
      AND status = p_tenant_status
  ) THEN
    RAISE EXCEPTION 'bootstrap tenant conflicts with existing row';
  END IF;

  INSERT INTO public.api_keys (id, tenant_id, kind, key_hash, key_prefix, created_at)
  VALUES (p_key_id, p_tenant_id, 'tenant', p_key_hash, p_key_prefix, now())
  ON CONFLICT (id) DO NOTHING;
  GET DIAGNOSTICS key_rows = ROW_COUNT;

  -- The bootstrap key is a tenant principal that holds the platform
  -- AdministratorAccess preset, so it can manage tenants and keys across the
  -- installation. It is not kind='platform': it still belongs to the default
  -- tenant, and row level security keeps every tenant's business data out of
  -- its reach regardless of what its policies allow it to call.
  INSERT INTO public.key_policies (key_id, policy_id, bound_by)
  VALUES (p_key_id, '00000000000000000000000010', 'bootstrap')
  ON CONFLICT (key_id, policy_id) DO NOTHING;

  IF key_rows = 0 AND NOT EXISTS (
    SELECT 1 FROM public.api_keys
    WHERE id = p_key_id
      AND tenant_id = p_tenant_id
      AND key_prefix = p_key_prefix
  ) THEN
    RAISE EXCEPTION 'bootstrap API key conflicts with existing row';
  END IF;

  RETURN QUERY SELECT tenant_rows = 1, key_rows = 1;
END
$$;

-- Cross-tenant API key lookup for admin revocation: locate the owning tenant of
-- an arbitrary key before the tenant context is known. api_keys has RLS, so a
-- direct SELECT returns zero rows without app.tenant_id set; this runs as the
-- table owner and bypasses RLS, like the other two lookup functions above.
CREATE OR REPLACE FUNCTION orbitjob_find_key_tenant(p_key_id text)
RETURNS TABLE(tenant_id text)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
  SELECT ak.tenant_id::text FROM public.api_keys ak
  WHERE ak.id = p_key_id AND ak.revoked_at IS NULL
$$;

REVOKE ALL ON FUNCTION public.orbitjob_auth_api_key(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.orbitjob_list_active_tenant_ids() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.orbitjob_bootstrap_default(text,text,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.orbitjob_find_key_tenant(text) FROM PUBLIC;

ALTER FUNCTION public.orbitjob_auth_api_key(text) OWNER TO orbitjob_table_owner;
ALTER FUNCTION public.orbitjob_list_active_tenant_ids() OWNER TO orbitjob_table_owner;
ALTER FUNCTION public.orbitjob_bootstrap_default(text,text,text,text,text,text,text) OWNER TO orbitjob_table_owner;
ALTER FUNCTION public.orbitjob_find_key_tenant(text) OWNER TO orbitjob_table_owner;

GRANT EXECUTE ON FUNCTION public.orbitjob_auth_api_key(text) TO orbitjob_admin;
GRANT EXECUTE ON FUNCTION public.orbitjob_list_active_tenant_ids() TO orbitjob_runtime;
GRANT EXECUTE ON FUNCTION public.orbitjob_bootstrap_default(text,text,text,text,text,text,text) TO orbitjob_admin;
GRANT EXECUTE ON FUNCTION public.orbitjob_find_key_tenant(text) TO orbitjob_admin;

-- ============================================================
-- Triggers
-- ------------------------------------------------------------
-- Only tables that are actually UPDATEd carry set_updated_at. The previous
-- baseline also had a pg_notify trigger on job_instances to wake the legacy
-- dispatcher; job_instances is gone, so notify_job_event is not carried over.
-- ============================================================
DROP TRIGGER IF EXISTS trg_tenants_set_updated_at ON tenants;
CREATE TRIGGER trg_tenants_set_updated_at
  BEFORE UPDATE ON tenants
  FOR EACH ROW
  EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_resource_groups_set_updated_at ON resource_groups;
CREATE TRIGGER trg_resource_groups_set_updated_at
  BEFORE UPDATE ON resource_groups
  FOR EACH ROW
  EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_policies_set_updated_at ON policies;
CREATE TRIGGER trg_policies_set_updated_at
  BEFORE UPDATE ON policies
  FOR EACH ROW
  EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_checks_set_updated_at ON checks;
CREATE TRIGGER trg_checks_set_updated_at
BEFORE UPDATE ON checks
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_slis_updated_at ON slis;
CREATE TRIGGER trg_slis_updated_at
BEFORE UPDATE ON slis
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_slos_updated_at ON slos;
CREATE TRIGGER trg_slos_updated_at
BEFORE UPDATE ON slos
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ============================================================
-- Ownership: every table, partition and sequence belongs to
-- orbitjob_table_owner, a NOLOGIN role. The migration runner assumes it with
-- SET ROLE, which is also why the platform presets below can be inserted under
-- RLS: the owner is not subject to its own ENABLE-only policies (ADR 0001).
-- ============================================================
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
END
$$;

-- ============================================================
-- Row level security
-- ------------------------------------------------------------
-- ENABLE only, never FORCE (ADR 0001). The table owner is NOLOGIN, so no
-- process queries as the owner and the owner-bypass path FORCE would close is
-- unreachable. Every policy is bound to current_setting('app.tenant_id', true),
-- which is unset unless the writer set it inside the transaction.
--
-- A partition does not inherit its parent's policy. audit_events_default
-- therefore repeats the policy of its parent audit_events; without it a caller
-- with table privileges can read every tenant by naming the partition (ADR 0002,
-- reproduced: 0 rows via the parent, 49,167 across 51 tenants via the partition).
-- ============================================================

ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenants_tenant ON tenants;
CREATE POLICY tenants_tenant ON tenants
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (id = current_setting('app.tenant_id', true))
  WITH CHECK (id = current_setting('app.tenant_id', true));

ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS api_keys_tenant ON api_keys;
CREATE POLICY api_keys_tenant ON api_keys
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE resource_groups ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS resource_groups_tenant ON resource_groups;
CREATE POLICY resource_groups_tenant ON resource_groups
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE policies ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS policies_tenant ON policies;
CREATE POLICY policies_tenant ON policies
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

-- Platform presets carry tenant_id IS NULL and must stay readable by every
-- tenant. Permissive policies combine with OR, so this widens reads only.
DROP POLICY IF EXISTS policies_platform_read ON policies;
CREATE POLICY policies_platform_read ON policies
  FOR SELECT TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id IS NULL);

-- key_policies has no tenant_id of its own: a binding is visible exactly when
-- the key it belongs to is visible.
ALTER TABLE key_policies ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS key_policies_tenant ON key_policies;
CREATE POLICY key_policies_tenant ON key_policies
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (EXISTS (
    SELECT 1 FROM api_keys ak
    WHERE ak.id = key_policies.key_id
      AND ak.tenant_id = current_setting('app.tenant_id', true)
  ))
  WITH CHECK (EXISTS (
    SELECT 1 FROM api_keys ak
    WHERE ak.id = key_policies.key_id
      AND ak.tenant_id = current_setting('app.tenant_id', true)
  ));

ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS audit_events_tenant ON audit_events;
CREATE POLICY audit_events_tenant ON audit_events
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

-- The partition carries its own RLS and its own policy. See the section comment.
ALTER TABLE audit_events_default ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS audit_events_default_tenant ON audit_events_default;
CREATE POLICY audit_events_default_tenant ON audit_events_default
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE job_definition_revisions ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS job_definition_revisions_tenant ON job_definition_revisions;
CREATE POLICY job_definition_revisions_tenant ON job_definition_revisions
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE job_run_control_plane ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS job_run_control_plane_tenant ON job_run_control_plane;
CREATE POLICY job_run_control_plane_tenant ON job_run_control_plane
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE job_run_attempts_control_plane ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS job_run_attempts_tenant ON job_run_attempts_control_plane;
CREATE POLICY job_run_attempts_tenant ON job_run_attempts_control_plane
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE checks ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS checks_tenant ON checks;
CREATE POLICY checks_tenant ON checks
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE check_runs ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS check_runs_tenant ON check_runs;
CREATE POLICY check_runs_tenant ON check_runs
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE slis ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS slis_tenant ON slis;
CREATE POLICY slis_tenant ON slis
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE slos ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS slos_tenant ON slos;
CREATE POLICY slos_tenant ON slos
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE sli_snapshots ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS sli_snapshots_tenant ON sli_snapshots;
CREATE POLICY sli_snapshots_tenant ON sli_snapshots
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE budgets ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS budgets_tenant ON budgets;
CREATE POLICY budgets_tenant ON budgets
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE budget_alerts ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS budget_alerts_tenant ON budget_alerts;
CREATE POLICY budget_alerts_tenant ON budget_alerts
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

-- ============================================================
-- Grants
-- ------------------------------------------------------------
-- RLS policies name roles but grant nothing. Without the grants below, a role
-- would pass every unit test and still be unable to read its own tables in a
-- real installation.
--
-- There is deliberately no ALTER DEFAULT PRIVILEGES ... TO orbitjob_admin. A
-- blanket default privilege would hand the HTTP role INSERT/UPDATE/DELETE on
-- every table a future migration creates, including a future ledger table --
-- exactly the failure the run-ledger rule below exists to prevent. A new table
-- gets orbitjob_admin access only when a migration grants it by name.
-- ============================================================
GRANT USAGE ON SCHEMA public TO orbitjob_admin, orbitjob_runtime, orbitjob_operator;

-- orbitjob_admin serves HTTP. It reads and writes the tables the API owns.
--
-- It gets SELECT -- and only SELECT -- on the three run-ledger tables:
--   job_definition_revisions, job_run_control_plane,
--   job_run_attempts_control_plane
-- Run rows are created by the operator, which owns the tables, in the same
-- transaction as the attempt and the audit row (PLAN.md D1). No process that
-- serves HTTP may write the ledger, because the product's claim is that the
-- ledger cannot be forged by a compromised component. If a later change adds
-- INSERT/UPDATE/DELETE for orbitjob_admin on any of these three tables, that
-- claim no longer holds: do not read the missing write grant as a bug to fix.
GRANT SELECT, INSERT, UPDATE, DELETE ON
  tenants, api_keys, key_policies, policies, resource_groups,
  audit_events, audit_events_default,
  checks, check_runs, slis, slos, sli_snapshots, budgets, budget_alerts
  TO orbitjob_admin;
GRANT SELECT ON
  job_definition_revisions, job_run_control_plane, job_run_attempts_control_plane
  TO orbitjob_admin;

-- orbitjob_runtime is the operator's login identity (OPERATOR_DSN) and also the
-- identity the legacy scheduler/dispatcher use for the checks and SLO tables.
-- It holds the ledger writes.
GRANT SELECT, INSERT, UPDATE, DELETE ON
  job_definition_revisions, job_run_control_plane, job_run_attempts_control_plane,
  checks, check_runs, slis, slos, sli_snapshots, budgets, budget_alerts
  TO orbitjob_runtime;
GRANT SELECT, INSERT ON audit_events, audit_events_default TO orbitjob_runtime;
-- The scheduler's quota check reads the tenant row before scheduling.
GRANT SELECT ON tenants TO orbitjob_runtime;

-- orbitjob_operator is a narrow, currently NOLOGIN identity kept for a future
-- dedicated operator credential. It gets the ledger writes and nothing else.
GRANT SELECT, INSERT, UPDATE, DELETE ON
  job_definition_revisions, job_run_control_plane, job_run_attempts_control_plane
  TO orbitjob_operator;

GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO orbitjob_admin;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO orbitjob_runtime;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO orbitjob_operator;

REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA public TO orbitjob_table_owner;

-- ============================================================
-- Catalog assertions
-- ------------------------------------------------------------
-- The previous baseline checked RLS against a hand-written list of table
-- names. A list cannot see a partition: audit_events_default is a separate
-- relation that never appeared in the list, so it could run without RLS while
-- the assertion reported success. The two rules below derive the protected set
-- from the catalog instead, so they keep working when a table or a partition is
-- added and they fail when RLS is removed from a relation the schema means to
-- protect. An assertion that cannot fail on a real defect is the defect this
-- project keeps producing, so it is written to fail on a relation, not to
-- confirm a list.
-- ============================================================

-- BEGIN structural catalog assertion
DO $$
DECLARE
  offender text;
BEGIN
  -- Rule 1: partition closure. PostgreSQL does not apply a parent's policy when
  -- a partition is queried directly, so a partition of an RLS-protected table
  -- must carry its own RLS (ADR 0002). This is the rule the named list was
  -- trying to express, and the one it could not.
  SELECT child.relname INTO offender
  FROM pg_inherits i
  JOIN pg_class child ON child.oid = i.inhrelid
  JOIN pg_class parent ON parent.oid = i.inhparent
  JOIN pg_namespace n ON n.oid = child.relnamespace
  WHERE n.nspname = 'public'
    AND parent.relrowsecurity
    AND NOT child.relrowsecurity
  LIMIT 1;

  IF offender IS NOT NULL THEN
    RAISE EXCEPTION 'baseline catalog assertion failed: partition % of an RLS-protected table has no RLS', offender;
  END IF;

  -- Rule 2: every tenant-owned relation has RLS enabled. Partitions inherit
  -- their parent's columns, so rule 1 and rule 2 both see a partition; rule 2's
  -- job is the tenant-owned table that never turned RLS on at all. Membership
  -- is derived from the tenant_id column, not a name. tenants is the one
  -- exception -- it is keyed by id, not tenant_id -- and is named once here;
  -- it cannot grow into a list because every other tenant-owned relation must
  -- carry the column.
  SELECT c.relname INTO offender
  FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname = 'public'
    AND c.relkind IN ('r', 'p')
    AND NOT c.relrowsecurity
    AND (
      c.relname = 'tenants'
      OR EXISTS (
        SELECT 1 FROM pg_attribute a
        WHERE a.attrelid = c.oid
          AND a.attname = 'tenant_id'
          AND a.attnum > 0
          AND NOT a.attisdropped
      )
    )
  LIMIT 1;

  IF offender IS NOT NULL THEN
    RAISE EXCEPTION 'baseline catalog assertion failed: tenant-owned relation % has no RLS', offender;
  END IF;
END
$$;
-- END structural catalog assertion

-- Existence check. Unlike the RLS rules above, "these relations must exist" can
-- only be expressed by naming them -- there is nothing in the catalog to derive
-- the expected set from. It guards against a CREATE that silently did not run.
DO $$
DECLARE
  missing_count integer;
BEGIN
  SELECT count(*) INTO missing_count
  FROM unnest(ARRAY[
    'tenants','api_keys','key_policies','policies','resource_groups',
    'audit_events','audit_events_default',
    'job_definition_revisions','job_run_control_plane','job_run_attempts_control_plane',
    'checks','check_runs','slis','slos','sli_snapshots','budgets','budget_alerts'
  ]) AS expected(name)
  WHERE to_regclass('public.' || expected.name) IS NULL;

  IF missing_count <> 0 THEN
    RAISE EXCEPTION 'baseline catalog assertion failed: % expected relations are missing', missing_count;
  END IF;
END
$$;

-- ============================================================
-- Platform preset policies
-- Description:
--   tenant_id IS NULL marks a preset: every tenant may bind it, none may edit
--   or delete it. Action lists are written out in full except for the
--   administrator preset, because a wildcard silently absorbs every action
--   added later -- the same reason Kubernetes RBAC forbids wildcard verbs.
--   The "self" token in resource patterns is replaced with the binding key's
--   own tenant at bind time, so one row serves every tenant.
--
--   An action earns a place here only when the HTTP API enforces it: a preset
--   action with no route grants nothing and makes the preset lie about what a
--   holder can do. instance:Cancel is the one exception, carried ahead of its
--   route landing.
-- ============================================================
INSERT INTO policies (id, tenant_id, name, description, document) VALUES
  ('00000000000000000000000010', NULL, 'AdministratorAccess',
   'Full control over every resource in every tenant. Held by the bootstrap key.',
   '{"version":"1","statement":[{"effect":"Allow","action":["*"],"resource":["orbitjob:*:*:*/*"]}]}'),

  ('00000000000000000000000011', NULL, 'TenantAdminAccess',
   'Full control inside one tenant, including policy, group and key management.',
   '{"version":"1","statement":[{"effect":"Allow","action":["tenant:Get","tenant:List","apikey:Create","apikey:List","apikey:Revoke","policy:Create","policy:Get","policy:List","policy:Delete","group:Create","group:List","job:Get","job:List","job:Trigger","instance:Get","instance:List","instance:Cancel","check:Create","check:Get","check:List","check:Delete","check:Pause","check:Resume","checkrun:Get","checkrun:List","sli:Create","sli:Get","sli:List","sli:Delete","slo:Create","slo:Get","slo:List","slo:Delete","slo:Pause","slo:Resume","slo:GetBudget","slo:ListBudgets","sloalert:Get","sloalert:List"],"resource":["orbitjob:self:*:*/*"]}]}'),

  ('00000000000000000000000012', NULL, 'ReadOnlyAccess',
   'Read access to every resource, without the ability to change anything.',
   '{"version":"1","statement":[{"effect":"Allow","action":["job:Get","job:List","instance:Get","instance:List","check:Get","check:List","checkrun:Get","checkrun:List","sli:Get","sli:List","slo:Get","slo:List","slo:GetBudget","slo:ListBudgets","sloalert:Get","sloalert:List","tenant:Get","group:List","policy:Get","policy:List","apikey:List"],"resource":["orbitjob:self:*:*/*"]}]}')
ON CONFLICT (id) DO NOTHING;

COMMIT;

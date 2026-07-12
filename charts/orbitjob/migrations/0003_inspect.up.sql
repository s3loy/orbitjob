BEGIN;

-- ============================================================
-- Table: checks
-- Description:
--   Stores inspection/check definitions managed by the inspection
--   scheduler. A check represents a health/metric/SSL inspection
--   that produces execution records in check_runs.
-- ============================================================
CREATE TABLE IF NOT EXISTS checks (

  -- Unique identifier of the check definition
  id BIGSERIAL PRIMARY KEY,

  -- Human readable check name
  name VARCHAR(128) NOT NULL,

  -- Optional description
  description TEXT,

  -- Tenant identifier for multi-tenant isolation
  tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',

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
  tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',

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
-- Indexes
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
-- Trigger: set_updated_at on checks
-- ============================================================
DROP TRIGGER IF EXISTS trg_checks_set_updated_at ON checks;
CREATE TRIGGER trg_checks_set_updated_at
BEFORE UPDATE ON checks
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

-- ============================================================
-- RLS Policies
-- ============================================================
DROP POLICY IF EXISTS checks_tenant_isolation ON checks;
CREATE POLICY checks_tenant_isolation ON checks
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id'))
    WITH CHECK (tenant_id = current_setting('app.tenant_id'));

DROP POLICY IF EXISTS check_runs_tenant_isolation ON check_runs;
CREATE POLICY check_runs_tenant_isolation ON check_runs
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id'))
    WITH CHECK (tenant_id = current_setting('app.tenant_id'));

COMMIT;

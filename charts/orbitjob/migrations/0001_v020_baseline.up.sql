BEGIN;

-- pgcrypto provides gen_random_uuid() which is used to generate run identifiers
-- ============================================================
-- Table: jobs
-- Description:
--   Stores job definitions (logical tasks) managed by the scheduler.
--   A job represents the configuration of a recurring or manual task.
--   Each job may produce multiple execution instances in job_instances.
-- ============================================================
CREATE TABLE IF NOT EXISTS jobs (

  -- Unique identifier of the job definition
  id BIGSERIAL PRIMARY KEY,

  -- Human readable job name (not necessarily unique)
  name VARCHAR(128) NOT NULL,

  
  -- Tenant identifier for multi-tenant isolation
  tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',

  -- Scheduling priority (higher value means higher priority)
  priority INT NOT NULL DEFAULT 0,

  -- Logical partition key for sharding/tenant isolation
  partition_key VARCHAR(64),

  -- Type of trigger mechanism
  -- cron   : scheduled execution using cron expression
  -- manual : triggered only via API/manual request
  trigger_type VARCHAR(16) NOT NULL DEFAULT 'cron',

  -- Cron expression defining execution schedule
  -- Required when trigger_type = 'cron'; for manual jobs it may be NULL/ignored
  cron_expr VARCHAR(64),

  -- Timezone used to evaluate cron schedule
  timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',

  -- Handler implementation type
  -- e.g. http, rpc, queue, script, worker-function
  handler_type VARCHAR(32) NOT NULL,

  -- Handler configuration payload
  -- Flexible JSON allowing handler-specific parameters
  handler_payload JSONB NOT NULL DEFAULT '{}'::jsonb,

  -- Maximum execution time allowed for a job instance
  timeout_sec INT NOT NULL DEFAULT 60,

  -- Maximum retry attempts after failure
  retry_limit INT NOT NULL DEFAULT 2,

  -- Delay between retries in seconds
  retry_backoff_sec INT NOT NULL DEFAULT 0,

  -- Retry backoff strategy
  -- fixed       : constant retry interval
  -- exponential : interval grows exponentially
  retry_backoff_strategy VARCHAR(16) NOT NULL DEFAULT 'fixed',

  -- Concurrency control policy when previous instance still running
  -- allow   : run concurrently
  -- forbid  : skip new execution
  -- replace : cancel previous instance and start new one
  concurrency_policy VARCHAR(16) NOT NULL DEFAULT 'allow',

  -- Behavior when scheduler misses execution time (misfire)
  -- skip     : ignore missed schedule
  -- fire_now : execute immediately
  -- catch_up : run all missed schedules
  misfire_policy VARCHAR(16) NOT NULL DEFAULT 'fire_now',

  -- Current job status
  -- active : scheduler will generate instances
  -- paused : scheduler will ignore job
  status VARCHAR(16) NOT NULL DEFAULT 'active',

  -- Next calculated execution time
  next_run_at TIMESTAMPTZ,

  -- Last time the scheduler created an instance
  last_scheduled_at TIMESTAMPTZ,

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

  -- Validate trigger type
  CONSTRAINT chk_jobs_trigger_type
    CHECK (trigger_type IN ('cron', 'manual')),

  -- Validate job status
  CONSTRAINT chk_jobs_status
    CHECK (status IN ('active', 'paused')),

  -- Validate retry backoff strategy
  CONSTRAINT chk_jobs_retry_backoff_strategy
    CHECK (retry_backoff_strategy IN ('fixed', 'exponential')),

  -- Validate concurrency policy
  CONSTRAINT chk_jobs_concurrency_policy
    CHECK (concurrency_policy IN ('allow', 'forbid', 'replace')),

  -- Validate misfire policy
  CONSTRAINT chk_jobs_misfire_policy
    CHECK (misfire_policy IN ('skip', 'fire_now', 'catch_up')),

  -- Validate handler type
  CONSTRAINT chk_jobs_handler_type
    CHECK (handler_type IN ('exec', 'http')),

  -- Enforce cron_expr when using cron trigger
  CONSTRAINT chk_jobs_cron_expr_required CHECK (
    (
      trigger_type = 'cron'
      AND cron_expr IS NOT NULL
      AND btrim(cron_expr) <> ''
    ) OR (
      trigger_type = 'manual'
      AND cron_expr IS NULL
    )
  ),

  -- Ensure numeric fields are valid
  CONSTRAINT chk_jobs_non_negative CHECK (
    priority >= 0 AND
    timeout_sec >= 1 AND
    retry_limit >= 0 AND
    retry_backoff_sec >= 0 AND
    version >= 1
  ),

  CONSTRAINT chk_jobs_tenant_id_non_empty CHECK (tenant_id <> ''),

  CONSTRAINT chk_jobs_handler_payload_object CHECK (jsonb_typeof(handler_payload) = 'object'),

  -- Support tenant-scoped composite foreign key reference from instances
  CONSTRAINT uq_jobs_tenant_id_id UNIQUE (tenant_id, id)
);

-- ============================================================
-- Table: job_instances
-- Description:
--   Stores execution records (instances) generated from jobs.
--   Each scheduled or manual execution corresponds to one row.
-- ============================================================
CREATE TABLE IF NOT EXISTS job_instances (

  -- Internal instance identifier
  id BIGSERIAL PRIMARY KEY,

  -- Unique execution run identifier
  -- Used for tracing, logging, and distributed execution
  run_id UUID NOT NULL DEFAULT gen_random_uuid(),

  -- Tenant identifier copied from jobs for tenant-level isolation/filtering
  tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',

  -- Associated job definition
  job_id BIGINT NOT NULL,

  -- Source of trigger (original source, immutable after creation)
  -- schedule : generated by scheduler
  -- manual   : triggered via API
  trigger_source VARCHAR(16) NOT NULL DEFAULT 'schedule',

  -- Scheduled execution time
  scheduled_at TIMESTAMPTZ NOT NULL,

  -- Current lifecycle state of instance
  status VARCHAR(16) NOT NULL DEFAULT 'pending',

  -- Instance-level priority snapshot copied from job at scheduling time
  priority INT NOT NULL DEFAULT 0,

  -- Effective priority with aging (materialized for index)
  effective_priority INT NOT NULL DEFAULT 0,

  -- Timestamp when dispatcher moved this instance to dispatched status
  dispatched_at TIMESTAMPTZ,

  -- Instance-level partition key copied from job
  partition_key VARCHAR(64),

  -- Client supplied idempotency key (mainly for manual trigger APIs)
  idempotency_key VARCHAR(128),

  -- Idempotency scope to avoid cross-endpoint/key collisions
  idempotency_scope VARCHAR(64) NOT NULL DEFAULT 'job_instance_create',

  -- Routing key for worker sharding / queue partition
  routing_key VARCHAR(128),

  -- Worker node currently executing the task
  worker_id VARCHAR(64),

  -- Current retry attempt number
  attempt INT NOT NULL DEFAULT 1,

  -- Maximum attempts allowed
  max_attempt INT NOT NULL DEFAULT 3,

  -- Actual execution start time
  started_at TIMESTAMPTZ,

  -- Execution finish time
  finished_at TIMESTAMPTZ,

  -- Lease expiration time for distributed worker locking
  lease_expires_at TIMESTAMPTZ,

  -- Next retry time if execution failed
  retry_at TIMESTAMPTZ,

  -- Result classification code
  result_code VARCHAR(32),

  -- Failure error message or diagnostic info
  error_msg TEXT,

  -- Distributed trace identifier
  trace_id VARCHAR(64),

  -- Creation timestamp
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  -- Last update timestamp
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  -- Optimistic locking version
  version INT NOT NULL DEFAULT 1,

  -- ------------------------------------------------------------
  -- Constraints
  -- ------------------------------------------------------------

  -- Validate trigger source
  CONSTRAINT chk_instances_trigger_source
    CHECK (trigger_source IN ('schedule', 'manual')),

  -- Validate execution status state machine
  CONSTRAINT chk_instances_status CHECK (
    status IN (
      'pending',
      'dispatched',
      'running',
      'retry_wait',
      'success',
      'failed',
      'canceled'
    )
  ),

  -- Ensure retry counters are consistent
  CONSTRAINT chk_instances_attempt CHECK (
    priority >= 0 AND
    attempt >= 1 AND
    max_attempt >= 1 AND
    attempt <= max_attempt
  ),

  -- Ensure timestamp fields are consistent with status.
  -- retry_wait is treated as "previous attempt finished, waiting next retry".
  CONSTRAINT chk_instances_status_timestamps CHECK (
    (
      status IN ('pending', 'dispatched')
      AND started_at IS NULL
      AND finished_at IS NULL
    ) OR (
      status = 'retry_wait'
      AND finished_at IS NOT NULL
      AND (started_at IS NULL OR finished_at >= started_at)
    ) OR (
      status = 'running'
      AND started_at IS NOT NULL
      AND finished_at IS NULL
    ) OR (
      status = 'success'
      AND started_at IS NOT NULL
      AND finished_at IS NOT NULL
      AND finished_at >= started_at
    ) OR (
      status IN ('failed', 'canceled')
      AND finished_at IS NOT NULL
      AND (started_at IS NULL OR finished_at >= started_at)
    )
  ),

  -- retry_wait must have a retry schedule.
  CONSTRAINT chk_instances_retry_wait_retry_at CHECK (
    status <> 'retry_wait' OR retry_at IS NOT NULL
  ),

  -- Support tenant-scoped composite foreign key reference from attempts.
  CONSTRAINT uq_instances_tenant_id_id UNIQUE (tenant_id, id),

  -- Ensure instance tenant matches parent job tenant
  CONSTRAINT fk_instances_job_tenant
    FOREIGN KEY (tenant_id, job_id) REFERENCES jobs(tenant_id, id),

  CONSTRAINT chk_instances_tenant_id_non_empty CHECK (tenant_id <> '')
);

-- ============================================================
-- Table: job_instance_attempts
-- Description:
--   Immutable per-attempt execution trail for retries/debugging.
--   job_instances remains the current state snapshot.
-- ============================================================
CREATE TABLE IF NOT EXISTS job_instance_attempts (
  id BIGSERIAL PRIMARY KEY,
  tenant_id VARCHAR(64) NOT NULL,
  instance_id BIGINT NOT NULL,
  attempt_no INT NOT NULL,
  worker_id VARCHAR(64),
  status VARCHAR(16) NOT NULL,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  result_code VARCHAR(32),
  error_msg TEXT,
  trace_id VARCHAR(64),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT chk_attempts_tenant_id_non_empty CHECK (tenant_id <> ''),
  CONSTRAINT chk_attempts_attempt_no CHECK (attempt_no >= 1),
  CONSTRAINT chk_attempts_status CHECK (status IN ('running', 'success', 'failed', 'canceled', 'timeout')),
  CONSTRAINT chk_attempts_time_order CHECK (
    (finished_at IS NULL) OR (started_at IS NULL) OR (finished_at >= started_at)
  ),
  CONSTRAINT uq_attempts_instance_attempt UNIQUE (tenant_id, instance_id, attempt_no),
  CONSTRAINT fk_attempts_instance_tenant FOREIGN KEY (tenant_id, instance_id)
    REFERENCES job_instances(tenant_id, id)
);

-- ============================================================
-- Table: workers
-- Description:
--   Worker registry for heartbeat, lease tracking and capacity metadata.
-- ============================================================
CREATE TABLE IF NOT EXISTS workers (
  -- Stable worker identifier generated by worker process at startup.
  -- Recommended format: UUID or "hostname:pid:boot_ts".
  worker_id VARCHAR(64) NOT NULL,
  tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  status VARCHAR(16) NOT NULL DEFAULT 'online',
  last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- No default by design: worker must set explicit lease deadline on heartbeat/register.
  lease_expires_at TIMESTAMPTZ NOT NULL,
  capacity INT NOT NULL DEFAULT 1,
  -- Worker labels for routing/selection.
  -- Example: {"region":"cn-east-1","queue":"video","gpu":"true"}.
  labels JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT pk_workers PRIMARY KEY (tenant_id, worker_id),
  CONSTRAINT chk_workers_tenant_id_non_empty CHECK (tenant_id <> ''),
  CONSTRAINT chk_workers_status CHECK (status IN ('online', 'offline', 'draining')),
  CONSTRAINT chk_workers_capacity CHECK (capacity >= 1)
);

-- ============================================================
-- Table: job_change_audits
-- Description:
--   Immutable audit records for job definition changes.
-- ============================================================
CREATE TABLE IF NOT EXISTS job_change_audits (
  id BIGSERIAL PRIMARY KEY,
  tenant_id VARCHAR(64) NOT NULL,
  job_id BIGINT NOT NULL,
  action VARCHAR(16) NOT NULL,
  changed_by VARCHAR(128) NOT NULL,
  before_hash VARCHAR(128),
  after_hash VARCHAR(128),
  -- Optional structured field-level diff for UI display/audit export.
  diff_payload JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT chk_job_audit_action CHECK (action IN ('create', 'update', 'pause', 'resume', 'delete')),
  CONSTRAINT chk_job_audit_tenant_non_empty CHECK (tenant_id <> ''),
  CONSTRAINT chk_job_audit_diff_payload_object CHECK (
    diff_payload IS NULL OR jsonb_typeof(diff_payload) = 'object'
  ),
  CONSTRAINT fk_job_audit_job FOREIGN KEY (tenant_id, job_id) REFERENCES jobs(tenant_id, id)
);

CREATE INDEX IF NOT EXISTS idx_job_change_audits_job_time
ON job_change_audits(tenant_id, job_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_attempts_instance_time
ON job_instance_attempts(tenant_id, instance_id, created_at DESC);
-- ============================================================
-- Prevent duplicate scheduled executions
-- Ensures scheduler does not create duplicate instances
-- for the same job at the same scheduled time.
-- ============================================================
CREATE UNIQUE INDEX IF NOT EXISTS uniq_schedule_instance
ON job_instances(tenant_id, job_id, scheduled_at)
WHERE trigger_source = 'schedule';

-- Prevent duplicate client requests when idempotency key is provided
CREATE UNIQUE INDEX IF NOT EXISTS uniq_instances_idempotency_key
ON job_instances(tenant_id, idempotency_scope, idempotency_key)
WHERE idempotency_key IS NOT NULL;

-- Unique run identifier index
CREATE UNIQUE INDEX IF NOT EXISTS uniq_job_instances_run_id
ON job_instances(run_id);

-- ============================================================
-- Scheduler scanning index
-- Used to efficiently locate runnable jobs by due time.
-- Priority ordering is handled in query sort/business logic.
-- ============================================================
CREATE INDEX IF NOT EXISTS idx_jobs_active_next_run
ON jobs(tenant_id, status, next_run_at)
WHERE deleted_at IS NULL AND next_run_at IS NOT NULL;

-- ============================================================
-- Retry scanning index
-- Allows retry scanner/scheduler to find instances waiting for retry.
-- Primary filter pattern: status='retry_wait' AND retry_at <= now().
-- ============================================================
CREATE INDEX IF NOT EXISTS idx_instances_retry_scan
ON job_instances(tenant_id, retry_at, scheduled_at)
WHERE status = 'retry_wait' AND retry_at IS NOT NULL;

-- Dispatcher scanning index for:
--   WHERE status IN ('pending','retry_wait')
--   ORDER BY effective_priority DESC, scheduled_at ASC
CREATE INDEX IF NOT EXISTS idx_instances_dispatch_scan
ON job_instances(tenant_id, status, effective_priority DESC, scheduled_at)
WHERE status IN ('pending', 'retry_wait');

-- Concurrency policy lookup index for forbid/replace checks:
--   WHERE tenant_id = ? AND job_id = ? AND status IN ('dispatched','running')
CREATE INDEX IF NOT EXISTS idx_instances_job_running
ON job_instances(tenant_id, job_id, status)
WHERE status IN ('dispatched', 'running');

-- Worker claim index: find dispatched instances ordered by effective priority.
CREATE INDEX IF NOT EXISTS idx_instances_dispatched_claim
ON job_instances(tenant_id, effective_priority DESC, scheduled_at)
WHERE status = 'dispatched';

-- Partition key lookup for shard-based routing.
CREATE INDEX IF NOT EXISTS idx_instances_partition_key
ON job_instances(tenant_id, partition_key)
WHERE partition_key IS NOT NULL;

-- Routing key lookup for worker queue routing.
CREATE INDEX IF NOT EXISTS idx_instances_routing_key
ON job_instances(tenant_id, routing_key)
WHERE routing_key IS NOT NULL;

-- Job-level instance lookup.
CREATE INDEX IF NOT EXISTS idx_instances_job_id
ON job_instances(job_id);

-- Orphan recovery and lease scanning for dispatched instances.
CREATE INDEX IF NOT EXISTS idx_instances_dispatched_lease
ON job_instances(status, lease_expires_at)
WHERE status = 'dispatched';

-- Orphan recovery and lease scanning for running instances.
CREATE INDEX IF NOT EXISTS idx_instances_running_lease
ON job_instances(status, lease_expires_at)
WHERE status = 'running';

-- ============================================================
-- Worker execution lookup index
-- Used for worker instance tracking
-- ============================================================
CREATE INDEX IF NOT EXISTS idx_instances_worker_status
ON job_instances(tenant_id, worker_id, status);

-- Worker lease lookup index
CREATE INDEX IF NOT EXISTS idx_workers_lease
ON workers(tenant_id, status, lease_expires_at);

-- ============================================================
-- Trigger Function: set_updated_at
-- Automatically updates updated_at on row modification
-- ============================================================
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Attach trigger to jobs table
DROP TRIGGER IF EXISTS trg_jobs_set_updated_at ON jobs;
CREATE TRIGGER trg_jobs_set_updated_at
BEFORE UPDATE ON jobs
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

-- Attach trigger to job_instances table
DROP TRIGGER IF EXISTS trg_job_instances_set_updated_at ON job_instances;
CREATE TRIGGER trg_job_instances_set_updated_at
BEFORE UPDATE ON job_instances
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

-- Attach trigger to workers table
DROP TRIGGER IF EXISTS trg_workers_set_updated_at ON workers;
CREATE TRIGGER trg_workers_set_updated_at
BEFORE UPDATE ON workers
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

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

DROP TRIGGER IF EXISTS trg_tenants_set_updated_at ON tenants;
CREATE TRIGGER trg_tenants_set_updated_at
  BEFORE UPDATE ON tenants
  FOR EACH ROW
  EXECUTE FUNCTION set_updated_at();

-- ============================================================
-- Table: api_keys
-- ============================================================
CREATE TABLE IF NOT EXISTS api_keys (
  id CHAR(26) PRIMARY KEY,
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id),
  key_hash VARCHAR(60) NOT NULL,
  key_prefix VARCHAR(12) NOT NULL,
  permissions JSONB NOT NULL DEFAULT '{}',
  expires_at TIMESTAMPTZ,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_by VARCHAR(64),
  CONSTRAINT chk_api_keys_key_hash_non_empty CHECK (key_hash <> ''),
  CONSTRAINT chk_api_keys_key_prefix_non_empty CHECK (key_prefix <> '')
);

CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys(tenant_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(key_prefix);

-- ============================================================
-- Table: audit_events (partitioned monthly, INSERT-only)
-- ============================================================
CREATE TABLE IF NOT EXISTS audit_events (
  id BIGSERIAL,
  tenant_id VARCHAR(64) NOT NULL,
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

DO $$
DECLARE
  required_role text;
BEGIN
  FOREACH required_role IN ARRAY ARRAY[
    'orbitjob_table_owner', 'orbitjob_migrator', 'orbitjob_admin',
    'orbitjob_runtime', 'orbitjob_operator', 'orbitjob_owner',
    'orbitjob_dispatcher', 'orbitjob_worker', 'orbitjob_reader'
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

-- Scheduler leader fencing token table.
-- Stores the current valid leader epoch per tenant.
-- Old rows are automatically cleaned up by lease expiry.
CREATE TABLE IF NOT EXISTS scheduler_leaders (
    tenant_id TEXT PRIMARY KEY,
    leader_epoch BIGINT NOT NULL,
    leader_id TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

-- Automatic cleanup of expired rows via background worker (PG 13+).
-- For older PG versions, rely on application-layer pruning or cron job.
CREATE INDEX IF NOT EXISTS idx_scheduler_leaders_expires_at ON scheduler_leaders(expires_at);

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

-- Migration: SLO/SLI system
-- Creates tables for SLI definitions, SLO targets, pre-aggregated snapshots,
-- error budget tracking, and burn rate alerts.

CREATE TABLE IF NOT EXISTS slis (
    id BIGSERIAL PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
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

DROP TRIGGER IF EXISTS trg_slis_updated_at ON slis;
CREATE TRIGGER trg_slis_updated_at
BEFORE UPDATE ON slis
FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE IF NOT EXISTS slos (
    id BIGSERIAL PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
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

DROP TRIGGER IF EXISTS trg_slos_updated_at ON slos;
CREATE TRIGGER trg_slos_updated_at
BEFORE UPDATE ON slos
FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE IF NOT EXISTS sli_snapshots (
    id BIGSERIAL PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
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
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
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
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
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

ALTER TABLE jobs DROP CONSTRAINT IF EXISTS chk_jobs_handler_type;
ALTER TABLE jobs ADD CONSTRAINT chk_jobs_handler_type
  CHECK (handler_type IN ('exec', 'http', 'webhook', 'pg_notify', 'container'));

-- ============================================================
-- NOTIFY/LISTEN trigger for dispatcher event-driven wake
-- ============================================================
CREATE OR REPLACE FUNCTION notify_job_event()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('job_events', '');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_job_instance_notify ON job_instances;
CREATE TRIGGER trg_job_instance_notify
AFTER INSERT OR UPDATE OF status ON job_instances
FOR EACH ROW
WHEN (NEW.status IN ('pending', 'retry_wait'))
EXECUTE FUNCTION notify_job_event();

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

ALTER TABLE jobs ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS jobs_tenant_v020 ON jobs;

CREATE POLICY jobs_tenant_v020 ON jobs
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE job_instances ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS job_instances_tenant_v020 ON job_instances;

CREATE POLICY job_instances_tenant_v020 ON job_instances
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE job_instance_attempts ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS job_instance_attempts_tenant_v020 ON job_instance_attempts;

CREATE POLICY job_instance_attempts_tenant_v020 ON job_instance_attempts
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE workers ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS workers_tenant_v020 ON workers;

CREATE POLICY workers_tenant_v020 ON workers
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS audit_events_tenant_v020 ON audit_events;

CREATE POLICY audit_events_tenant_v020 ON audit_events
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE job_change_audits ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS job_change_audits_tenant_v020 ON job_change_audits;

CREATE POLICY job_change_audits_tenant_v020 ON job_change_audits
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE checks ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS checks_tenant_v020 ON checks;

CREATE POLICY checks_tenant_v020 ON checks
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE check_runs ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS check_runs_tenant_v020 ON check_runs;

CREATE POLICY check_runs_tenant_v020 ON check_runs
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE slis ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS slis_tenant_v020 ON slis;

CREATE POLICY slis_tenant_v020 ON slis
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE slos ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS slos_tenant_v020 ON slos;

CREATE POLICY slos_tenant_v020 ON slos
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE sli_snapshots ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS sli_snapshots_tenant_v020 ON sli_snapshots;

CREATE POLICY sli_snapshots_tenant_v020 ON sli_snapshots
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE budgets ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS budgets_tenant_v020 ON budgets;

CREATE POLICY budgets_tenant_v020 ON budgets
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE budget_alerts ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS budget_alerts_tenant_v020 ON budget_alerts;

CREATE POLICY budget_alerts_tenant_v020 ON budget_alerts
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS api_keys_tenant_v020 ON api_keys;

CREATE POLICY api_keys_tenant_v020 ON api_keys
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;


DROP POLICY IF EXISTS tenants_tenant_v020 ON tenants;

CREATE POLICY tenants_tenant_v020 ON tenants
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (id = current_setting('app.tenant_id', true))
  WITH CHECK (id = current_setting('app.tenant_id', true));

ALTER DEFAULT PRIVILEGES FOR ROLE orbitjob_table_owner IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO orbitjob_admin;

GRANT USAGE ON SCHEMA public TO orbitjob_admin, orbitjob_runtime, orbitjob_operator;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO orbitjob_admin;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO orbitjob_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON jobs, job_instances, workers, checks, check_runs,
  slis, slos, sli_snapshots, budgets, budget_alerts, scheduler_leaders TO orbitjob_runtime;
GRANT SELECT, INSERT ON job_instance_attempts, audit_events, job_change_audits TO orbitjob_runtime;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO orbitjob_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON jobs TO orbitjob_operator;
GRANT SELECT, INSERT ON job_change_audits TO orbitjob_operator;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO orbitjob_operator;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA public TO orbitjob_table_owner;

-- Narrow cross-tenant entry points required before a tenant context is known.
CREATE OR REPLACE FUNCTION orbitjob_auth_api_key(p_prefix text)
RETURNS TABLE(id text, tenant_id text, key_hash text, revoked text, expired text)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
  SELECT ak.id::text, ak.tenant_id::text, ak.key_hash::text,
         CASE WHEN ak.revoked_at IS NOT NULL THEN 'revoked' END,
         CASE WHEN ak.expires_at IS NOT NULL AND ak.expires_at < now() THEN 'expired' END
  FROM public.api_keys ak
  JOIN public.tenants t ON t.id = ak.tenant_id
  WHERE ak.key_prefix = p_prefix
    AND ak.revoked_at IS NULL
    AND t.status = 'active'
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

REVOKE ALL ON FUNCTION public.orbitjob_auth_api_key(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.orbitjob_list_active_tenant_ids() FROM PUBLIC;
ALTER FUNCTION public.orbitjob_auth_api_key(text) OWNER TO orbitjob_table_owner;
ALTER FUNCTION public.orbitjob_list_active_tenant_ids() OWNER TO orbitjob_table_owner;
GRANT EXECUTE ON FUNCTION public.orbitjob_auth_api_key(text) TO orbitjob_admin;
GRANT EXECUTE ON FUNCTION public.orbitjob_list_active_tenant_ids() TO orbitjob_runtime;

-- Bootstrap is the only cross-tenant write path. The caller supplies a bcrypt
-- hash; plaintext API keys never enter PostgreSQL functions or logs.
CREATE OR REPLACE FUNCTION orbitjob_bootstrap_default(
  p_tenant_id text,
  p_tenant_slug text,
  p_tenant_name text,
  p_tenant_status text,
  p_key_id text,
  p_key_hash text,
  p_key_prefix text,
  p_permissions jsonb
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
     OR p_key_id <> '00000000000000000000000002'
     OR p_permissions <> '{}'::jsonb THEN
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

  INSERT INTO public.api_keys (id, tenant_id, key_hash, key_prefix, permissions, created_at)
  VALUES (p_key_id, p_tenant_id, p_key_hash, p_key_prefix, p_permissions, now())
  ON CONFLICT (id) DO NOTHING;
  GET DIAGNOSTICS key_rows = ROW_COUNT;

  IF key_rows = 0 AND NOT EXISTS (
    SELECT 1 FROM public.api_keys
    WHERE id = p_key_id
      AND tenant_id = p_tenant_id
      AND key_prefix = p_key_prefix
      AND permissions = p_permissions
  ) THEN
    RAISE EXCEPTION 'bootstrap API key conflicts with existing row';
  END IF;

  RETURN QUERY SELECT tenant_rows = 1, key_rows = 1;
END
$$;

REVOKE ALL ON FUNCTION public.orbitjob_bootstrap_default(text,text,text,text,text,text,text,jsonb) FROM PUBLIC;
ALTER FUNCTION public.orbitjob_bootstrap_default(text,text,text,text,text,text,text,jsonb) OWNER TO orbitjob_table_owner;
GRANT EXECUTE ON FUNCTION public.orbitjob_bootstrap_default(text,text,text,text,text,text,text,jsonb) TO orbitjob_admin;

DO $$
DECLARE
  missing_count integer;
BEGIN
  SELECT count(*) INTO missing_count
  FROM unnest(ARRAY[
    'tenants','api_keys','jobs','job_instances','job_instance_attempts','workers',
    'audit_events','job_change_audits','scheduler_leaders','checks','check_runs',
    'slis','slos','sli_snapshots','budgets','budget_alerts'
  ]) AS expected(name)
  WHERE to_regclass('public.' || expected.name) IS NULL;

  IF missing_count <> 0 THEN
    RAISE EXCEPTION 'v0.2.0 catalog assertion failed: % expected tables are missing', missing_count;
  END IF;

  IF EXISTS (
    SELECT 1 FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public'
      AND c.relname = ANY(ARRAY[
        'tenants','api_keys','jobs','job_instances','job_instance_attempts','workers',
        'audit_events','job_change_audits','checks','check_runs','slis','slos',
        'sli_snapshots','budgets','budget_alerts'
      ])
      AND NOT c.relrowsecurity
  ) THEN
    RAISE EXCEPTION 'v0.2.0 catalog assertion failed: tenant table lacks RLS';
  END IF;
END
$$;

COMMIT;

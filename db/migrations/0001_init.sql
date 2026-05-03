BEGIN;

-- pgcrypto provides gen_random_uuid() which is used to generate run identifiers
CREATE EXTENSION IF NOT EXISTS pgcrypto;

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
  retry_limit INT NOT NULL DEFAULT 0,

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
  misfire_policy VARCHAR(16) NOT NULL DEFAULT 'skip',

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
  max_attempt INT NOT NULL DEFAULT 1,

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
      'dispatching',
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
      status IN ('pending', 'dispatching', 'dispatched')
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
--   ORDER BY priority DESC, scheduled_at ASC
CREATE INDEX IF NOT EXISTS idx_instances_dispatch_scan
ON job_instances(tenant_id, status, effective_priority DESC, scheduled_at)
WHERE status IN ('pending', 'retry_wait');

-- Concurrency policy lookup index for forbid/replace checks:
--   WHERE tenant_id = ? AND job_id = ? AND status IN ('dispatching','running')
CREATE INDEX IF NOT EXISTS idx_instances_job_running
ON job_instances(tenant_id, job_id, status)
WHERE status IN ('dispatched', 'running');

-- Worker claim index: find dispatched instances ordered by effective priority.
CREATE INDEX IF NOT EXISTS idx_instances_dispatched_claim
ON job_instances(tenant_id, effective_priority DESC, scheduled_at)
WHERE status = 'dispatched';

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

COMMIT;

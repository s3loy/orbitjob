BEGIN;

-- ADR 0012: preserve the original execution identity and evidence gap across
-- CR recreation, cancellation and operator restarts. Historical ownership is
-- unknown until the matching Job provides verifiable evidence; never infer it
-- from the UID of a recreated JobRun. Existing terminal history is unchanged.
ALTER TABLE job_run_attempts_control_plane
  ADD COLUMN owner_run_uid VARCHAR(128),
  ADD COLUMN execution_unknown_since TIMESTAMPTZ,
  ADD COLUMN execution_unknown_reason VARCHAR(64),
  ADD COLUMN termination_evidence JSONB,
  ADD CONSTRAINT chk_attempt_termination_evidence_object
    CHECK (termination_evidence IS NULL OR jsonb_typeof(termination_evidence) = 'object'),
  ADD CONSTRAINT chk_attempt_owner_run_uid_nonempty
    CHECK (owner_run_uid IS NULL OR btrim(owner_run_uid) <> ''),
  ADD CONSTRAINT chk_attempt_unknown_reason
    CHECK (execution_unknown_reason IS NULL OR execution_unknown_reason IN
      ('JobNotFound', 'JobUIDMismatch', 'PodIdentityAmbiguous'));

-- Bound recovery scans use tenant-scoped keyset reads, including inactive
-- sources and runs whose Kubernetes representations have disappeared.
CREATE INDEX idx_job_run_execution_recovery
  ON job_run_control_plane (tenant_id, id)
  WHERE attempt > 0 AND phase IN
    ('CreatingAttempt', 'Running', 'ExecutionUnknown', 'CancelRequested', 'CancelUnknown');

-- This is additive table data, under the existing owner/RLS/grants. Runtime
-- and operator remain the execution writers; Admin gains no write privilege.
-- Stop old operator writers before migration and keep Unknown-aware writers
-- during rollback. Do not clear unknown evidence to run an older binary.
COMMIT;

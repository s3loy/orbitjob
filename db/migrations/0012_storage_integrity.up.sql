BEGIN;

-- Restore the append-only application audit contract. Offline maintenance
-- continues to use the migration/table-owner identity. Existing and future
-- partition grants must be reviewed with the parent (ADR 0002/0005).
DO $$
DECLARE
  relation_name text;
BEGIN
  FOR relation_name IN
    SELECT relid::regclass::text FROM pg_partition_tree('public.audit_events'::regclass)
  LOOP
    EXECUTE 'REVOKE UPDATE, DELETE, TRUNCATE ON TABLE ' || relation_name ||
      ' FROM orbitjob_admin, orbitjob_runtime, orbitjob_operator';
  END LOOP;
END
$$;

-- A terminal phase remains the execution fact. This nullable acknowledgement
-- only tracks completion of its derived projections, making missed hooks
-- discoverable after a process restart; it is not a second execution queue.
ALTER TABLE job_run_control_plane
  ADD COLUMN IF NOT EXISTS outcome_recorded_at TIMESTAMPTZ;

-- Accepted compatibility cutover: preserve existing aggregates. Historical
-- runs predate the dedup facts, so replay cannot tell an already-counted event
-- from a missed hook. Stop old operators before migration; only new terminal
-- transitions participate in automatic recovery after this boundary.
UPDATE job_run_control_plane SET outcome_recorded_at=transaction_timestamp()
WHERE phase IN ('Succeeded','Failed','Canceled') AND outcome_recorded_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_job_run_outcome_pending
  ON job_run_control_plane (tenant_id, id)
  WHERE phase IN ('Succeeded', 'Failed', 'Canceled') AND outcome_recorded_at IS NULL;

-- Snapshot increments need an idempotency fact per run and SLI. These facts
-- outlive pruned ledger rows so an outcome replay cannot increment twice.
CREATE TABLE IF NOT EXISTS sli_snapshot_events (
  tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  run_id BIGINT NOT NULL,
  sli_id BIGINT NOT NULL REFERENCES slis(id),
  window_start TIMESTAMPTZ NOT NULL,
  is_good BOOLEAN NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, run_id, sli_id)
);

ALTER TABLE sli_snapshot_events OWNER TO orbitjob_table_owner;
ALTER TABLE sli_snapshot_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY sli_snapshot_events_tenant ON sli_snapshot_events
  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
  USING (tenant_id = current_setting('app.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true));
GRANT SELECT ON sli_snapshot_events TO orbitjob_admin;
GRANT SELECT, INSERT ON sli_snapshot_events TO orbitjob_runtime, orbitjob_operator;

-- References must remain within the tenant even though PostgreSQL FK checks
-- bypass RLS. Group authorization is checked and locked in the writer's tx.
ALTER TABLE slis ADD CONSTRAINT uq_slis_tenant_id_id UNIQUE (tenant_id, id);
ALTER TABLE slos ADD CONSTRAINT fk_slos_sli_tenant
  FOREIGN KEY (tenant_id, sli_id) REFERENCES slis(tenant_id, id);
ALTER TABLE sli_snapshot_events ADD CONSTRAINT fk_sli_snapshot_events_sli_tenant
  FOREIGN KEY (tenant_id, sli_id) REFERENCES slis(tenant_id, id);

COMMIT;

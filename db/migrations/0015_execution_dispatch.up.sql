BEGIN;

-- Stop older execution writers before this migration. A historical empty UID
-- is an uncertain send, never proof that no Kubernetes request was issued.
ALTER TABLE job_run_attempts_control_plane
  ADD COLUMN dispatch_stage VARCHAR(32),
  ADD COLUMN dispatch_marked_at TIMESTAMPTZ;

UPDATE job_run_attempts_control_plane
SET dispatch_stage = CASE WHEN COALESCE(kubernetes_job_uid, '') <> ''
                         THEN 'Bound' ELSE 'DispatchMayHaveStarted' END,
    dispatch_marked_at = CURRENT_TIMESTAMP;

ALTER TABLE job_run_attempts_control_plane
  ALTER COLUMN dispatch_stage SET DEFAULT 'Prepared',
  ALTER COLUMN dispatch_stage SET NOT NULL,
  ADD CONSTRAINT chk_attempt_dispatch_stage CHECK
    (dispatch_stage IN ('Prepared', 'DispatchMayHaveStarted', 'Bound')),
  ADD CONSTRAINT chk_attempt_dispatch_identity CHECK
    ((dispatch_stage = 'Bound') = (COALESCE(kubernetes_job_uid, '') <> '')),
  ADD CONSTRAINT chk_attempt_dispatch_clock CHECK
    (dispatch_stage = 'Prepared' OR dispatch_marked_at IS NOT NULL);

-- Existing identity-observation paths bind the stage with the immutable UID.
-- This invoker trigger grants no new table access and preserves existing RLS.
CREATE FUNCTION guard_execution_dispatch() RETURNS TRIGGER
LANGUAGE plpgsql SET search_path = public, pg_temp AS $$
BEGIN
  IF TG_OP = 'UPDATE' THEN
    IF OLD.dispatch_stage <> 'Prepared' AND NEW.dispatch_stage = 'Prepared' THEN
      RAISE EXCEPTION 'execution dispatch cannot return to Prepared';
    END IF;
    IF OLD.dispatch_stage = 'Bound' AND
       NEW.kubernetes_job_uid IS DISTINCT FROM OLD.kubernetes_job_uid THEN
      RAISE EXCEPTION 'bound execution UID is immutable';
    END IF;
  END IF;
  IF COALESCE(NEW.kubernetes_job_uid, '') <> '' THEN
    NEW.dispatch_stage := 'Bound';
  END IF;
  IF NEW.dispatch_stage <> 'Prepared' THEN
    NEW.dispatch_marked_at := COALESCE(NEW.dispatch_marked_at, CURRENT_TIMESTAMP);
  END IF;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION guard_execution_dispatch() FROM PUBLIC;
CREATE TRIGGER guard_execution_dispatch
  BEFORE INSERT OR UPDATE ON job_run_attempts_control_plane
  FOR EACH ROW EXECUTE FUNCTION guard_execution_dispatch();

-- The existing Runtime identity remains the only execution writer. Admin has
-- no new write privilege. Keep the stage on rollback and use compatible code.
COMMIT;

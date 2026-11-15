BEGIN;

-- Only a claimed HTTP operation receives the short-lived proof. Store its hash
-- so a forged AdmissionReview cannot reserve an arbitrary object UID.
ALTER TABLE definition_operations ADD COLUMN admission_nonce_hash CHAR(64);
ALTER TABLE definition_operations ADD CONSTRAINT definition_admission_hash
  CHECK (admission_nonce_hash IS NULL OR admission_nonce_hash ~ '^[0-9a-f]{64}$');

COMMIT;

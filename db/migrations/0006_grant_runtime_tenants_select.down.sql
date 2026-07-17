-- Reverts 0006_grant_runtime_tenants_select.up.sql
REVOKE SELECT ON tenants FROM orbitjob_runtime;

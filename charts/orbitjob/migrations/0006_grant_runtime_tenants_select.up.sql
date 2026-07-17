-- 0006: scheduler quota check reads tenant rows
--
-- checkConcurrentInstanceQuota (internal/core/store/postgres/scheduler_repository.go)
-- runs `SELECT quotas FROM tenants WHERE id = $1` as orbitjob_runtime before
-- every scheduled instance insert. The tenants RLS policy already names
-- orbitjob_runtime, but the SELECT grant itself was missing, so every cron
-- scheduling attempt failed with 42501 and landed in the backoff path.
--
-- Scope stays minimal: read-only, tenant-scoped by the existing policy.

GRANT SELECT ON tenants TO orbitjob_runtime;

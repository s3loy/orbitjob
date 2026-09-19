# Status

What is delivered, what is gone, and what is still moving. Written 2026-09-18
against this branch. Structure is described in
[architecture.md](architecture.md); this page records behaviour, and every
claim in it was read from the tree on the day of writing. Parts of the branch
are landing while this page stands, so the dates matter.

## What OrbitJob is now

A Kubernetes-only run ledger. A tenant declares work as a `ScheduledJob`
custom resource; due occurrences and manual triggers become `JobRun` custom
resources, the operator renders each one into a Kubernetes Job, and it writes
one ledger row per run and per attempt into PostgreSQL in the same
transaction. The admin API reads the ledger and cannot write it. The
scheduler evaluates each active tenant's SLIs against its error budgets on a
five-minute loop. Tenant isolation is row level security in the database;
authorization is a policy engine over API keys.

## Delivered

- Single-baseline schema. `db/migrations/0001_baseline.up.sql` is the
  authority: row level security on the tenant-owned tables, ULID (`CHAR(26)`)
  tenant ids, and a SELECT-only grant for `orbitjob_admin` on the three
  ledger tables. `charts/orbitjob/migrations/` is a synced copy checked by
  `make helm-check`.
- Migration 0002 (`0002_checks_to_kubernetes_jobs.up.sql`) cut checks over to
  Kubernetes Jobs: `job_run_control_plane.scheduled_for`, `slis.source_type`
  cutover to `job_run`, and `sli_snapshots` truncated so windows accrue from
  real executions only.
- Migration 0003 (`0003_resource_group_id_on_ledger.up.sql`) added nullable
  `resource_group_id` to both ledger tables, stamped with group lineage from
  check to revision to run.
- Checks execute as Kubernetes Jobs. The operator's check loop runs under the
  same Lease as its schedule loop (`cmd/operator/main.go`), renders probe
  Jobs from a digest-pinned curl image
  (`internal/core/app/projection/check.go`), and one recorder derives the
  check-run read model, SLI snapshots and check metrics from each run's
  terminal outcome (`internal/core/app/checkobserve/recorder.go`).
- Cancel is a `spec.cancelRequested` patch on the JobRun. A run whose custom
  resource is missing answers 409 Conflict, never 404
  (`internal/admin/kube/jobrun.go`), and a run already in a terminal phase is
  final: the cancel repeats its phase instead of patching
  (`internal/admin/app/run/command/cancel.go`).
- Governance gates. Admin routes sit behind the policy action each route
  declares (`internal/admin/http/permission_router.go`), group-scoped keys
  are refused on the job and run surfaces
  (`resource.RequireUnscoped`, `internal/domain/resource/errors.go`), and the
  admin-api's identity is a per-namespace Role holding create/get/patch on
  jobruns and workflowruns and nothing else
  (`charts/orbitjob/templates/admin-rbac.yaml`) — workflowruns being the
  in-flight Workflows feature's kind.
- Load tooling on the CR path. `scripts/loadtest` generates ScheduledJob
  manifests, verifies the ledger and that terminal runs still have their
  JobRun resources (`scripts/loadtest/verify.go`), and rejects images without
  pinned digests (`scripts/loadtest/images.go`). The `make loadtest-*`
  targets drive preflight through a full profile run.
- Monitoring. `make monitoring-up` installs kube-prometheus-stack from
  `deploy/monitoring/values-kube-prometheus-stack.yaml`, plus the repo's own
  PodMonitors, ServiceMonitor and PrometheusRule in
  `deploy/monitoring/orbitjob-observability.yaml` and a postgres-exporter.
  `make observability-status` reports what is live.

## Removed

The legacy in-process pipeline is gone: the dispatcher and worker processes,
the devserver, and the exec, http, webhook, pg_notify and container handlers
they ran, along with the `jobs`, `job_instances` and `job_instance_attempts`
tables underneath them. Tenant execution modes went with the pipeline
(`control_plane_tenant_mode`, the runtime switch endpoint, writer epoch
fencing). The compose stacks and the hand-rolled monitoring configs
(Prometheus, Grafana, Loki, promtail) were replaced by the
kube-prometheus-stack install. The etcd discovery registry and config
watcher are gone. The baseline migration's header comments record what was
dropped and why.

## In flight

None of this is landed. Each item is mid-stream in the tree or an approved
design awaiting implementation.

- The interval split. `OPERATOR_SCHEDULE_INTERVAL_SEC` (seconds) drives the
  schedule and check loops, which tick together by design
  (`cmd/operator/main.go`); retention has its own knob,
  `OPERATOR_RETENTION_INTERVAL_MIN` (minutes), since it is housekeeping rather
  than scheduling. Giving the check loop its own knob is still open.
- The scheduler carries an optional etcd election path (`ETCD_ENABLED`,
  `cmd/scheduler/main.go`) that only builds with the `etcd` build tag, which
  no shipped artifact sets. Delete it or wire it; the decision is open.
- The GitHub loadtest workflow (`.github/workflows/loadtest.yml`) has never
  completed a profile end to end: the first run died building
  `cmd/operator/` (untracked), the next two on `prepare` timing out because
  ScheduledJob CRs never gained an `activeRevision` — operator logs captured
  by the workflow's failure dump show a healthy-looking loop (cache synced,
  leadership taken, schedule ticks firing) with no reconcile errors against
  the load namespaces, which points at the CRs never reaching the operator's
  hands (namespace, tenancy or CRD install) rather than a reconcile defect.
  Open; the dump now carries the evidence to decide it.
- Coverage debt: nine tier packages sit below their thresholds and off the
  baseline (`make test-cover-check`), including the new
  `internal/core/app/projection` at 70.3%. Baseline lines are deleted, never
  added, so this closes by fixing packages, one at a time.

## Delivered since this page was written (2026-09-18, later the same day)

- Functions and Workflows are landed and committed (`93d1be8`): the consuming
  loops exist (`functionsync`, the workflow walker), the HTTP surface is live
  (`/api/v1/functions/*`, `/api/v1/workflows/*`, migration 0006 arming the
  six TenantAdmin actions), and `internal/operator/` watches five resource
  kinds. The "undelivered" note above was true when written and is false now;
  README's feature list no longer says otherwise.
- The pre-release image path exists (`.github/workflows/prerelease.yml`):
  branch builds reach GHCR as `rc-<run>-<sha>` plus an `rc` alias without a
  repository tag.

## Known issues

The list lives in [known-issues.md](known-issues.md) and is not duplicated
here. Two notes as of 2026-09-18:

- Fixed in the tree since entries were written there: `make kind-verify` no
  longer asserts a hardcoded migration count
  (`deploy/kind/verify-install.sh` now derives the versions from the
  directory and fails on gaps), and the load tool's Kubernetes-mode gap is
  closed by the CR path described above, including removal of the inert
  `X-OrbitJob-Tenant` header.
- Superseded by the removals: the writer-epoch and migration-0005 entries
  describe a table and a migration that no longer exist.

## Where to look

The docs index lives in [README](../README.md).

- [USAGE.md](../USAGE.md) - deployment, API examples, operations
- [architecture.md](architecture.md) - components, processes, data flow
- [database-setup.md](database-setup.md) - PostgreSQL, TLS, Secret contract
- [local-deployment.md](local-deployment.md) - kind development guide
- [loadtest-guide.md](loadtest-guide.md) - the load tool and its profiles
- [known-issues.md](known-issues.md) - open defects with evidence
- [api/openapi.yaml](../api/openapi.yaml) - the HTTP contract
- [CONTRIBUTING.md](../CONTRIBUTING.md) - branches, tests, migrations
- [SECURITY.md](../SECURITY.md) - security model, roles, RLS

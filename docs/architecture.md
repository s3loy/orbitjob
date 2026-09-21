# Architecture

What OrbitJob is, structurally. This document describes **shape**, not status: it
says what exists and where, not whether it works. Behaviour verdicts live in
[status.md](status.md); decisions and their consequences live in
`docs/superpowers/adr/` until they are moved here.

Everything stated below was read from the code on 2026-09-18. Claims that could
not be checked say so.

## What it is

A Kubernetes-only run ledger. OrbitJob answers three questions about scheduled
work — did it run, how many times, who triggered it. A tenant declares work as
a `ScheduledJob` custom resource (a cron expression or nothing, a container
image, a retry policy), and the platform materializes it, runs it when due or
on a manual trigger, retries it, and records every attempt in the PostgreSQL
ledger. Tenant isolation is enforced in the database with row level security,
and authorization is a policy engine over API keys rather than a role on the
key. Kubernetes is the only execution path; the legacy in-process pipeline that
ran handlers inside OrbitJob processes was removed.

## Processes

Nine `cmd/` entrypoints. Three are long-running; the rest are tooling.

| Process | Role |
|---|---|
| `admin-api` | The HTTP API. Tenant, key, policy, resource group, check and SLO management; read access to job definitions and runs; manual trigger; cancel. SELECT-only against the database. |
| `scheduler` | Evaluates each active tenant's SLI snapshots against its error budgets on a five-minute loop and raises burn-rate alerts. It creates nothing. |
| `operator` | Fires due schedules and due checks, reconciles `ScheduledJob` and `JobRun` custom resources into Kubernetes Jobs, and writes the run ledger plus the check-run read model. |
| `configure` | Install-time CLI: `configure setup` generates the installation database state (bundled or external mode). |
| `bootstrap` | Creates the first tenant and API key, then writes the key to a SecretWriter (local file or Kubernetes Secret). |
| `migrate` | Applies `db/migrations`. Runs as a Helm hook. |
| `openapi-gen` | Regenerates `api/openapi.yaml` from the handler definitions. |
| `healthcheck` | Development and test helper. |
| `orbitjob` | Developer CLI for an installed system: `checks`, `doctor`, `jobs`, `runs` and `status` subcommands. A client only — it talks to the admin API over HTTP and to the cluster through kubectl, and never opens a database connection. Exit codes 0, 1, 3 (not found) and 4 (auth). |

## Layers

```
internal/
  platform/     config, dbconfig, election, health, logger, metrics,
                migrate, postgrestest, scan
  core/         domain -> app -> store/postgres
  admin/        app -> store/postgres -> http
  operator/     the CRD reconciler
  domain/       types shared across layers: resource, validation
api/kubernetes/ CRD type definitions (ScheduledJob, JobRun)
db/migrations/  schema authority
```

The dependency rule is one-way: `admin` and `operator` may import `core`;
`core` may not import either. The compiler only catches the mutual case as an
import cycle — a one-way `core` → `admin` import builds fine and quietly inverts
the architecture. Check it directly:

```bash
go list -f '{{.ImportPath}} -> {{join .Imports " "}}' ./internal/core/... \
  | grep -E "orbitjob/internal/(admin|operator)"
```

On 2026-09-18 that returns nothing. `operator` imports
`core/app/checkobserve`, `core/app/checkschedule`, `core/app/controlplane`,
`core/app/execution`, `core/app/projection`, `core/domain/jobrun` and
`core/domain/revision`, which is the permitted direction.

## Where state lives

PostgreSQL 17 is the only durable store. There is no cache and no second
source of truth.

- **Tenant-owned tables** carry a `tenant_id` and row level security. The GUC
  `app.tenant_id` is what the policies compare against; a connection that does
  not set it sees nothing.
- **`job_definition_revisions`** holds the materialized job definitions. The
  operator projects each `ScheduledJob` custom resource into a revision here.
- **`job_run_control_plane`** and **`job_run_attempts_control_plane`** are the
  run ledger: one row per run and one per attempt, written by the operator in
  the same transaction as it renders the Kubernetes Job. `orbitjob_admin` has
  SELECT-only on all three tables — the API cannot write the ledger, which is
  the product's anti-forgery claim.
- **`audit_events`** is append-only and partitioned by `created_at`. It is the
  only partitioned table.

Schema lives in `db/migrations/`: the baseline
(`0001_baseline.up.sql`) plus `0002` (checks cut over to Kubernetes Jobs),
`0003` (`resource_group_id` lineage on the ledger), `0004` (functions),
`0005` (workflows) and `0006` (the tenant-admin actions that arm the
function/workflow routes) — 20 relations carrying row level security. It is the
authority; `charts/orbitjob/migrations/` is a generated copy, and `make
helm-check` fails when the two drift.

## Execution

Kubernetes is the only execution path. The operator's schedule loop creates a
`JobRun` custom resource for every due occurrence, its check loop does the same
for due checks — the probe Jobs render from a digest-pinned curl image
(`internal/core/app/projection/check.go:18`) — and the admin API creates one
for a manual trigger; the operator renders each `JobRun` into a Kubernetes
`batch/v1` Job from the definition's job template and records the run and its
attempts in the ledger. A terminal run also upserts the check-run read model
and the SLI snapshots from that outcome
(`internal/core/app/checkobserve/recorder.go`). A cancel is a patch of
`spec.cancelRequested` on the JobRun; the operator deletes the Kubernetes Job
and closes the run as `Canceled`. The admin API's cluster identity is a
per-namespace Role holding create/get/patch on `jobruns` and `workflowruns`,
and nothing else (`charts/orbitjob/templates/admin-rbac.yaml`). A ledger row
whose custom resource is missing resolves to `409 Conflict`, never `404`
(`internal/admin/kube/jobrun.go:148`) — the API distinguishes "gone" from
"never existed".

## Interfaces

**HTTP.** `api/openapi.yaml` (6,997 lines) is generated from the handler
definitions and checked in. `make openapi-check` proves the file matches its
generator. It does not prove either matches the handlers — a generator that
omits a field makes both wrong together.

**Kubernetes.** Four CRDs — `ScheduledJob`, `JobRun`, `WorkflowJob`,
`WorkflowRun` — defined under `api/kubernetes/` and installed from
`charts/orbitjob/crds/`. The operator reconciles all four: the workflow kinds
have reconcilers in `internal/operator/` (`workflow.go`, `workflow_walker.go`,
`workflow_retention.go`), the controller watches five resource kinds including
`workflowruns` and `workflowjobs`, and `cmd/operator` runs the workflow
advance, retention and function-sync loops under leader election
(`internal/core/app/functionsync/`).

## Deployment

A Helm chart at `charts/orbitjob/`. Installation is ordered by hook weight, low
first: `owner-init` creates and constrains the database roles (`-30`), the
migrations ConfigMap is written (`-25`), `migrate` applies the schema (`-20`) —
all on `pre-install,pre-upgrade`; the workloads come up; then `bootstrap` runs on
`post-install,post-upgrade` (`-10`) to create the first tenant and key.

The chart takes a Secret name and a fixed set of key names for the database; it
does not take a password per role.

The chart installs the control plane into one namespace (`orbitjob-system` in
the guides) plus a task namespace (`orbitjob-tasks`) for the Kubernetes Jobs it
renders. The kind development flow adds two more: `orbitjob` holds PostgreSQL,
`monitoring` holds Prometheus and Grafana.

The chart requires `operator.namespaceTenants` — a mapping from namespace to
tenant. It has no default on purpose: a default would bind a namespace to a
tenant nobody chose.

## Shape of the codebase

Read on 2026-09-18, excluding `.git/` and `.claude/`:

| | Files | Lines |
|---|---|---|
| Production Go (`cmd/`, `internal/`, `api/`) | 245 | 32,255 |
| Test Go (`cmd/`, `internal/`, `api/`, `test/`, `db/`) | 240 | 45,201 |
| `api/openapi.yaml` | 1 | 6,997 |

Test code is about 1.4× production code. That ratio is not by itself good or
bad; what it means depends on whether those tests run against the real
dependencies, which is a question for [status.md](status.md).

## Where the truth lives

| Fact | Source |
|---|---|
| Go version, dependencies | `go.mod` |
| HTTP contract | `api/openapi.yaml`, generated by `make openapi-gen` |
| Database schema | `db/migrations/` |
| Image tag, chart version | `charts/orbitjob/Chart.yaml` |
| Environment variables | `deploy/env/*.env.example`, `charts/orbitjob/values.yaml` |
| Commands and gates | `Makefile`, `CONTRIBUTING.md` |
| Decisions and their consequences | `docs/superpowers/adr/` |
| Known defects | `docs/known-issues.md` |
| What works, what does not | [status.md](status.md) |

## What this document does not claim

It does not say that any of the above works. Structure being present and
behaviour being correct are different claims, and this codebase has already been
bitten once by treating the first as the second. Where a capability's status is
not established, status.md says `unknown` rather than guessing.

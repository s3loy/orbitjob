# OrbitJob — Product Requirements

| | |
|---|---|
| Status | Draft — awaiting s3's review |
| Date | 2026-09-17 |
| Supersedes | `docs/superpowers/PRD-OrbitJob-Platform.md` (2026-07-12). That document described a much larger product — Workflow, Serverless, cross-cluster scheduling — and is the source of the "Kubernetes-first, deliver everything" identity this one narrows. |
| Decisions | Product shape settled by s3 on 2026-09-17: open-source product, one execution path (Kubernetes), checks/SLI/SLO kept as a differentiator. Amended by s3 on 2026-09-18: Functions and Workflows are approved in (designs at `docs/superpowers/plans/serverless-design.md` and `docs/superpowers/plans/workflow-design.md`; implementation in flight — the schema migrations are in the tree, the API surface is not). Amended by s3 on 2026-09-19: the Functions and Workflows implementation landed the same day it was approved — the schema migrations, the workflow CRDs and the HTTP surface (`/api/v1/functions/*`, `/api/v1/workflows/*` in `api/openapi.yaml`) are in the tree. |

## 1. What OrbitJob is

A multi-tenant job scheduler for Kubernetes. Tenants declare scheduled and
manual work as Custom Resources; an operator projects each declaration into a
Kubernetes Job; PostgreSQL holds the transaction state — who ran, when, with
what outcome, and what was retried.

It also does something schedulers usually do not: it records service level
indicators, evaluates SLOs against them, and reports error budgets and alerts
from inside the scheduler rather than beside it.

## 2. Who uses it

Platform teams who run scheduled workloads for several internal tenants on
Kubernetes, and who want tenant isolation enforced somewhere stronger than
application code.

They are not data engineers building DAGs. There is no pipeline concept here,
no data passing between steps, no backfill of a dependency graph.

## 3. The one execution path

**Every tenant runs in Kubernetes mode. The legacy mode is removed, not kept as
a compatibility path.**

This resolves the dual-mode design that produced the largest share of the
current defects: two state locations, a runtime switch, a writer epoch, and
verification split across two shapes. After this change there is one
declaration surface (the CRD), one state location (`job_run_control_plane` and
its attempt table), and one thing to reason about per tenant.

Consequences that must be implemented, not assumed:

- The operator reads the tenant's mode. Today it does not, and the runtime
  switch therefore changes only the legacy half.
- `control_plane_tenant_mode`, the writer epoch and the switch endpoint are
  deleted rather than left half-wired.
- Anything that currently reaches state through `job_instances` — the load
  tool, the verifier, the admin API's instance endpoints — is rewritten against
  the control-plane tables or removed.

All three are done as of 2026-09-18, and they were done by deletion: the
legacy half the bullets describe is gone outright, so there is no tenant mode
left to read, `control_plane_tenant_mode`, the writer epoch and the switch
endpoint have no trace in the tree, and `grep -rn "job_instances"
--include='*.go' internal cmd` matches only comments.

## 4. What we are betting on

Two properties, and only these two, are the reason someone would choose this
over Kestra or Argo Workflows.

**4.1 Tenant isolation enforced by the database.** Not "we are multi-tenant" —
that is table stakes — but "isolation holds even when a query forgets its
tenant predicate, because the database refuses the row". Row level security,
not application-level filtering. A competitor cannot copy this without
changing where their state lives.

**4.2 SLIs, SLOs and error budgets inside the scheduler.** Elsewhere this is a
second stack — Prometheus, Sloth, Grafana — wired to the first by hand. Here it
is one system and one query surface.

When this was written both were claims rather than properties. Section 8
defines what makes them real, and its table records where each stands.

## 5. The five-minute path

An open-source product is judged in its first five minutes. This is a hard
requirement, and it is the constraint that decides what must exist:

> One command brings up a working OrbitJob with no prerequisite beyond a
> Kubernetes cluster and a container runtime. It opens a page. That page shows
> the system running — tenants, schedules, runs, their states — and lets the
> reader submit a job and watch it succeed.

Today the path is one command (`bash scripts/quickstart.sh`), whose seven steps
need Docker, kind, kubectl, helm, make, Go and netcat on the machine
(`scripts/quickstart.sh:9`). It still depends on a Secret that only a separate
helper creates (`make kind-db`) — the dependency is real, and it is now stated
where it bites: `docs/local-deployment.md` step 2 says to run it before
`helm install` and names the hook failure skipping it causes. It ends at a
port-forward and a `curl`. There is no page at all: no templates, no embedded
assets, no frontend of any kind in the repository.

Making the sentence above true is a larger piece of work than any single defect
in `docs/status.md`, and it is the one that decides adoption.

## 6. Scope

**In.** Tenant and API key management; cron and manual triggers; per-job
concurrency and misfire policy; container job execution as a Kubernetes Job;
retries with backoff, timeouts, cancellation; run history and attempt records;
checks; SLIs, SLOs, budgets and alerts; authorization by explicit grant.

**Out, deliberately.** Cross-cluster scheduling; dual authentication; storing
per-attempt state, leases or logs in the CRD; anything that makes this a
message queue or an event store.

"Out" here means we will not build it and will not carry scaffolding for it.

Workflow and Serverless were in this list when it was written. The previous PRD
listed them as future versions; on 2026-09-17 that roadmap was withdrawn and
both were out. On 2026-09-18 s3 approved both back in — Workflow as DAG
execution on the run ledger, Functions as HTTP-invoked one-shot runs — with the
designs at `docs/superpowers/plans/workflow-design.md` and
`docs/superpowers/plans/serverless-design.md`. Implementation is landed as of
2026-09-19: the schema migrations and the workflow CRDs are in the tree
(`db/migrations/0004_functions.up.sql`, `0005_workflows.up.sql`), and the HTTP
surface for both is live in `api/openapi.yaml` (`/api/v1/functions/*`,
`/api/v1/workflows/*`). For what remains out, the original meaning holds.

## 7. What is explicitly not a requirement

- Exactly-once execution. The semantics are at-least-once; handlers must
  tolerate duplicate invocation with a stable idempotency key.
- Sub-millisecond scheduling.
- Horizontal scale beyond what one PostgreSQL and one operator can express.
- A plugin or integration ecosystem. Kestra ships 1900+ integrations; we are
  not competing on that axis.

## 8. Definition of done

**None of these checks existed when the table was written (2026-09-17).** The
first version of this table named six checks as if they were gates; every one
of them was work to build. What follows is therefore a work list, not a
scoreboard. Each row names the command that settles it, and the state column is
dated, so a row is falsifiable from the moment it is written rather than from
the moment it is finished. State as of 2026-09-18:

| Property | The check that must exist | State on 2026-09-18 |
|---|---|---|
| Tenant isolation is real | `make integration` runs against a schema built from `db/migrations/`, not from `internal/platform/postgrestest/schema.sql` | Check exists and passes. `postgrestest/schema.sql` is deleted; the suite builds from the migrations, and partition RLS is asserted — `db/migrations/schema_security_integration_test.go`, `db/migrations/tenant_visibility_integration_test.go` (`TestPartitionScopingIsLoadBearing`) |
| The five-minute path works | A timed job from a clean machine, one command to a running system with a page | Red. The path is one script (`scripts/quickstart.sh`) needing seven tools, and it ends at `curl`. No page exists in the repository |
| One execution path | No route to run state other than the control-plane tables | Check exists and passes. `grep -rn "job_instances" --include='*.go' internal cmd` matches only comments |
| SLIs and SLOs are a product, not a table | A run that overspends a budget produces an alert through the documented surface | Surface now exists — budget and alert endpoints in `api/openapi.yaml` (`/slos/{id}/budget`, `/slo-alerts`), and the scheduler's evaluation loop raises burn alerts (`cmd/scheduler/main.go`). Whether the original bar — an overspend demonstrated end to end — is met is not re-established here |
| The instruments can fail | A negative test per gate, proving it goes red when it should | Fixed. `printf 'mode: set\n' > /tmp/c.out && bash scripts/check-coverage.sh /tmp/c.out` now exits 1: `coverage gate failed: /tmp/c.out holds no coverage data`, `checked 0 of 44 tier package(s)` |
| Nothing is half-wired | Every exported function reachable from a running process has a production caller, or is deleted | The named examples are gone or wired: the epoch fence is deleted with the legacy path, operator cancellation is implemented (`internal/operator/cancel.go`), the etcd stack is behind its opt-in gate (`ETCD_ENABLED`, `cmd/scheduler/main.go`). The property remains a standing rule |

Two rules for this table, learned the hard way in this repository:

1. **A row may not cite a plan or a document as its check.** ADR 0007's status
   is accepted-but-not-yet-implemented. Citing it as proof that a
   gate exists is how the previous version of this table came to be wrong.
2. **A check that passes with no input is not a check.** If the source it reads
   can be empty, the check must say so rather than report success. See
   `docs/superpowers/adr/0008`.

## 9. Where we are

The gap between section 1 and the running system is the substance of
`docs/status.md`. The three findings below bore most directly on this PRD when
it was written; each carries its state as of 2026-09-18.

- **The chosen execution path is the least verified.** The operator carried two
  CRITICAL findings, could not be driven by the project's own load tool, and
  the integration schema did not contain its tables — while the legacy path,
  the one this PRD removes, was the only one that had completed an end-to-end
  smoke run. **State:** the premise dissolved by deletion. The legacy path is
  gone, the operator path is the only path, and the tooling now drives it end
  to end: the load tool declares `ScheduledJob` custom resources and verifies
  against `job_run_control_plane` and the JobRun CRs
  (`scripts/loadtest/generate.go`, `scripts/loadtest/verify.go`), and the
  integration suite builds its schema from `db/migrations/`.
- **The isolation claim is unproven, and the one test that touches it tests a
  different question.** The file this finding named
  (`internal/core/store/postgres/scheduler_repository_rls_test.go`) built its
  own ENABLE-and-FORCE policies on two legacy relations, and the shared test
  database was built from a schema with no RLS at all. **State:** resolved. The
  test file is deleted, `postgrestest/schema.sql` with it; the suite builds
  from `db/migrations/` under the production ENABLE-only policies, and the
  isolation tests are structural —
  `db/migrations/tenant_visibility_integration_test.go` asserts every
  tenant-owned table filters cross-tenant reads and that partition scoping is
  load-bearing; `db/migrations/schema_security_integration_test.go` asserts
  each partition carries its own RLS and policy.
- **The product has no surface.** There is no page, no SDK, no CLI for users.
  The API is the only way in, which means the five-minute path cannot currently
  be written. **State:** still true.

This section is a statement of position, not a plan. Its purpose is that the
distance is not discovered later.

## 10. Risks

**We are deleting the only path that works.** That is a deliberate trade: two
half-working paths have cost more than one working path would have. The
mitigation was ordering — the end-to-end gate a prerequisite for deleting
legacy, not a consequence of it — and as of 2026-09-18 the ordering held:
legacy is deleted, and the Kubernetes path is the one the load tool, the
integration suite and the smoke test all run against. What remains of this risk
is the narrower one the load-test record still shows: beyond the smoke profile,
the path's evidence is thin.

**The five-minute path is the largest single item here**, larger than any
defect, and it is the one nobody has started. If it slips, the product stays
unusable regardless of how correct it becomes.

## 11. Open questions

- Which namespace does the operator watch, and how does a tenant get one? The
  current answer is a hand-maintained namespace-to-tenant mapping passed as an
  environment variable, which does not scale to an open-source adopter with
  twenty tenants.
- Is the five-minute path a Helm install, or a single binary for a laptop? The
  answer decides whether "only a Kubernetes cluster" is honest.

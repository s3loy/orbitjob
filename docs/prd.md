# OrbitJob product requirements

OrbitJob provides task scheduling, execution management and reliability
monitoring for teams operating tenant workloads on Kubernetes. This document
defines product scope and acceptance requirements. An implementation or test result
does not by itself change a requirement. [Project status](status.md)
tracks implementation; [verification](verification.md) records test results.

## Product and users

Platform teams need to declare work, control access, inspect attempts and
outcomes, retain diagnostic output and evaluate service objectives. Operators
need to identify who requested a run, which revision executed and why an
execution succeeded, failed or remains uncertain.

Kubernetes executes containers. PostgreSQL stores pinned definitions, execution
facts, management state and derived SLO models. Application grants, database RLS
and namespace RBAC enforce different parts of isolation; see
[architecture](architecture.md) and [security](../SECURITY.md).

## Product scope

- Same-origin Console, HTTP API and developer CLI for workload management and
  operational inspection.
- Tenant, API-key, policy and resource-group management with explicit grants,
  delegation boundaries and tenant/group-scoped queries.
- ScheduledJob cron/manual runs, concurrency and misfire policies, pinned
  revisions, deadlines, retries, cancellation and run/attempt history.
- Workflow DAGs with dependencies, conditional gating, timezone-aware automatic
  schedules and cancellation across child executions.
- Versioned container Functions with create/edit, pause/resume, parameterless
  invocation and history.
- API-managed HTTP checks executed as Jobs, SLI snapshots, SLO evaluation,
  error budgets and burn alerts.
- Bounded execution-log archives with retention, visible collection gaps,
  independent read/delete permissions and audited cleanup.
- Generated OpenAPI, Prometheus metrics, structured logs and Helm deployment.

ScheduledJob and WorkflowJob definitions remain Kubernetes custom resources.
Typed Admin API routes read, validate and apply them within configured tenant
namespaces. Function definitions are managed through their dedicated API.
Definition acceptance and asynchronous operator projection are separate states.

## Onboarding

The [first-use guide](getting-started.md) guides a user through joining an existing installation and running an authorized job; authoring
requires separate definition permissions. The [development guide](local-deployment.md)
provisions a separate kind cluster, prepares PostgreSQL and credentials, builds
the images and installs the chart. Existing clusters use Helm with an explicitly
configured database and tenant namespace mapping.
Installation duration depends on the environment and image-build cache.

## Execution and ownership requirements

External execution is at least once. Retries, ambiguous transport results and
Kubernetes Job backoff can repeat workload effects. Workloads must make effects
idempotent when repetition is unacceptable.

The Admin API writes management state and publishes desired state; the runtime
owns execution facts. Before reporting cancellation complete, the operator must
confirm that the owned workload stopped. Missing resources alone cannot prove
termination. Unknown executions retain their concurrency slot and block
new dependent workflow work.

Source admission must serialize concurrent requests with definition lifecycle
changes. Replays return the original occurrence. Workflow parent stop must
prevent new child execution. Terminal projection retries must preserve failures
and avoid double-counting SLI events.

Historical aggregates are preserved at the migration cutover. Automatic recovery
covers post-cutover terminal transitions; missing historical projections require
an offline assessment. Deduplication facts outlive ordinary run pruning. See
[database upgrade requirements](database-setup.md#writer-cutover).

## Acceptance requirements

Acceptance covers authorization and tenant isolation, transactional auditing,
concurrent admission, creation and cancellation recovery, retained output,
SLI/SLO calculations and installation behavior. Database boundaries require
real PostgreSQL tests; Kubernetes behavior requires controlled cluster scenarios.
Console workflows require browser checks against the applicable API profile.

Each result records its source, environment and exercised behavior in
[verification](verification.md). Missing observations remain unknown, and
readiness alone does not establish business recovery.

## Deliberate limits and open work

Cross-cluster scheduling, arbitrary workflow dataflow, a general integration
marketplace, sub-millisecond scheduling and exactly-once external effects are
outside the product scope. Workflow dependencies do not automatically pass task
output to downstream tasks. Function invocation accepts no arbitrary payload.

[Known issues](known-issues.md) tracks implementation gaps and operating
constraints, including installed fault qualification, schedule-adherence SLI derivation
and retirement conditions for deduplication facts. Namespace provisioning
remains an explicit installation operation.

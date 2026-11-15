# Architecture

OrbitJob separates workload execution, durable execution facts and management
operations. This document describes the implemented components and their
contracts. [Verification](verification.md) records tested behavior and limits.

## Components

OrbitJob executes tenant workloads through Kubernetes Jobs and records execution
facts in PostgreSQL. Kubernetes resources request work and carry desired state;
the run ledger records pinned revisions, attempts, identities and outcomes.
The removed in-process dispatcher/worker pipeline is not an execution fallback.

| Entrypoint | Responsibility |
|---|---|
| `cmd/admin-api` | Authenticated management API; reads definitions and history; creates/patches run CRs for trigger, invoke and cancellation. It writes management tables, but cannot write the execution ledger. |
| `cmd/operator` | Projects definitions, creates scheduled/check runs, synchronizes Functions, advances Workflows, ensures container Jobs, observes attempts, recovers projections and prunes history. |
| `cmd/scheduler` | Evaluates tenant SLI snapshots into SLO budgets and burn alerts. It does not create workload Jobs. |
| `cmd/admission` | Validates ScheduledJob and WorkflowJob writes against configured namespace ownership and definition-operation proofs. |
| `cmd/orbitjob-identity` | Installation operator tool that issues single-use account setup and recovery capabilities through the Admin database role. |
| `cmd/configure` | Generates installation database state and role credentials. |
| `cmd/migrate` | Applies ordered, checksummed SQL migrations under the migration identity. |
| `cmd/bootstrap` | Ensures the initial tenant/key and delivers the configured key to a local file or Kubernetes Secret. |
| `cmd/orbitjob` | HTTP/kubectl client for checks, jobs, runs, status and diagnostics; no direct database access. |
| `cmd/openapi-gen` | Generates or checks the OpenAPI document from route metadata and DTOs. |
| `cmd/healthcheck` | Health endpoint helper. |
| `cmd/log-archive` | Single-writer bounded execution evidence on a dedicated PVC; compact tenant-scoped PostgreSQL directory; node collector ingestion and internal reader access over mTLS. |

The archive uses the trusted Runtime DB identity; the Admin API has directory
read and audited cleanup-request privileges. Container output, lifecycle events
and manifests live on the log PVC. [Execution logs](execution-logs.md) defines
the collection, retention, quotas and operational boundaries.

## Account sessions and API packaging

The Admin API supports password-authenticated human sessions independently of
Bearer automation keys. Session writes require exact Origin and CSRF checks;
workspace workload access requires an explicit active membership and effective
policy grants. Installation-administrator status controls global accounts and
workspace metadata without granting implicit workload access. See
[accounts](accounts.md) and [credential boundaries](../SECURITY.md#browser-console).

The backend Docker image contains the Admin API and identity tool without a
compiled Console bundle. The optional `/console/` handler returns 503 if assets
are absent; API routes remain available. Backend source builds do not require
Node.js or frontend assets.

Typed definition routes apply only within configured tenant namespaces and
compare UID/resourceVersion. They publish Kubernetes desired state; the Operator
owns ledger writes. Separate log routes resolve immutable execution ownership
and return bounded retained text. Neither surface provides a generic Kubernetes
proxy. Platform preset documents use the narrowly granted migration 0007
reader; workspace discovery uses migration 0011's authorized metadata reader.
See [API usage](../USAGE.md#calling-the-api).

## Code boundaries

`internal/admin/http` adapts requests to admin application use cases and
repositories. `internal/operator` adapts Kubernetes events to core application
services and the ledger. `internal/core/domain` contains business models;
`internal/core/app` contains use cases and dependency interfaces;
`internal/core/store/postgres` implements persistence. Shared validation/resource
types live in `internal/domain`; infrastructure lives in `internal/platform`.

Admin and operator may import core. Production core code must not import admin
or operator. External-package integration tests may exercise both sides of that
boundary.

## Tenant and authorization boundaries

Bearer authentication resolves the key, tenant, resource group and effective
policy documents once per request. Protected routes declare an action and target
resource. Explicit Deny takes precedence; absent grants deny access. Boundary
intersection and delegation containment preserve the supported exact-action and
whole-segment resource-wildcard semantics and fail closed when proof limits are
exhausted. The bootstrap tenant does not itself confer cross-tenant access.

Trusted tenant/group/path fields are assigned by the server after client binding
or excluded from binding. Group-aware read models preserve their parent scope;
surfaces with no supported group restriction refuse scoped callers. SLI/SLO
references are checked within the write transaction and tenant foreign keys.
See [API keys and authorization](../SECURITY.md#api-keys-and-authorization).

## Persistent state

The complete [migration directory](../db/migrations/) defines schema, roles,
functions, RLS and grants. The [chart migration copy](../charts/orbitjob/migrations/)
is synchronized and checked; it is not a second schema definition.

- `job_definition_revisions` stores immutable executable snapshots. ScheduledJob
  and WorkflowJob projections, checks and Function synchronization use revisions.
- `job_run_control_plane` and `job_run_attempts_control_plane` store runs and
  attempts. `workflow_run_control_plane` stores parent runs and task decisions.
- `checks`, `check_runs`, `functions`, `function_runs`, SLIs, SLOs, snapshots,
  budgets and alerts provide management state and derived query models.
- `audit_events` and its partitions are append-only for application roles.
  A management/ledger write and its audit event commit together.
- `outcome_recorded_at` tracks completed terminal projections;
  `sli_snapshot_events` deduplicates each tenant/run/SLI increment in the same
  transaction as its snapshot update. These facts survive run pruning.

Repositories set transaction-local `app.tenant_id`; RLS filters application
roles, which do not own the tables. The table owner is NOLOGIN, the migrator
can assume it, and runtime processes use separate login identities. Admin has
read-only access to execution history, while retaining management-table writes.
Reviewed SECURITY DEFINER functions cover narrowly defined operations that must
resolve a tenant before a normal scoped transaction is available.

## Creation, observation and cancellation

1. Every entry point resolves a pinned revision and stable occurrence identity.
   API manual triggers and Function invocations publish run CRs for Operator
   admission. Scheduled executions and Workflow steps record their durable
   occurrence or step before publishing the CR. Idempotent publication and
   recovery converge on the stored identity after an interrupted operation.
2. Before an external creation request, the repository commits an empty-UID
   `CreatingAttempt` intent. A second transaction locks the run/attempt, checks
   current state and performs the bounded Job Ensure. PostgreSQL and Kubernetes
   do not share a transaction; the durable intent makes uncertain results visible.
3. A confirmed UID binds the attempt. Subsequent observations must match that
   immutable identity and advance resourceVersion/phase monotonically. Recovery
   uses the stored actual Job address, including a compatible historical name.
4. Workflow steps lock parent before child/attempt. A stopped or terminal parent
   refuses new steps, reservation and Ensure; cancellation cannot be overwritten
   by a stale Running update.
5. Cancellation patches run intent with UID/resourceVersion tests. The Operator
   shortens the original Job's deadline and waits for controller stop
   acknowledgement, commits verified owned-Pod termination evidence, releases
   its Pod evidence finalizers, then requests foreground Job deletion. An ambiguous
   creation remains `CancelUnknown`; absence of a known UID is not proof that
   nothing executed. Terminal outcomes remain final.

New run names retain complete occurrence digests. Historical short names are
bounded lookup candidates, accepted only after full identity checks. Retention
uses UID/resourceVersion delete preconditions and includes inactive sources and
workflow parent/child resources. See
[migration compatibility](database-setup.md#migration-lineage-and-compatibility).

External execution is at least once. `restartPolicy: Never` prevents restarts
inside a Pod; a Job's configured backoff and platform attempts may still start
workload code again. Workloads must tolerate repeated effects. Rendered Pods
disable service-account token mounting.

## Terminal recovery and SLOs

Terminal phase commits before derived check/function/SLI writes. Acknowledgement
follows all required idempotent projections. Failed projections remain pending
and protected from retention; per-tenant keyset scanning prevents a full failing
page from blocking later healthy rows. This recovery loop does not require
leadership because duplicate delivery is safe.

Migration 0012 preserves existing statistics and acknowledges old terminal rows;
newly terminal runs participate in automatic recovery after cutover. Old operators
must stop before migration. Recovery does not reconstruct unprovable historical
counts. The [database guide](database-setup.md#audit-permissions-and-outcome-recovery)
defines the cutover, rollback and deduplication-retention requirements.

HTTP checks run as digest-pinned container probes. Their actual run outcomes
produce check results and five-minute SLI buckets. The scheduler evaluates SLO
windows and burn alerts from those buckets. Supported criteria describe only
produced status/duration facts; schedule adherence has no dedicated SLI derivation.

## Interfaces and deployment

The HTTP contract is [OpenAPI](../api/openapi.yaml), served at `/openapi.json`.
`make openapi-check` verifies generated freshness; contract tests additionally
exercise JSON field names, required fields and versioned request bodies. The
generated `APIKey` security scheme declares the Bearer header; middleware and
policy evaluation enforce authentication and authorization.

Observability uses structured logs, Prometheus metrics and an echoed/generated
`X-Trace-ID` for request correlation. Distributed tracing spans and an
OpenTelemetry exporter are not implemented.

The four CRDs are `ScheduledJob`, `JobRun`, `WorkflowJob` and `WorkflowRun`, defined
under [api/kubernetes/](../api/kubernetes/) and installed from the [chart CRDs](../charts/orbitjob/crds/).
The operator watches these resources and Kubernetes Jobs. Its schedule/check,
Function synchronization, workflow and retention loops run under Kubernetes Lease
leadership. The scheduler has a separate optional, build-tagged etcd path.

Helm orders owner initialization, migration and bootstrap hooks around deployment
startup. Database Secret name/key settings come from chart values. Tenant workload
namespaces must appear in `operator.namespaceTenants`; RBAC is granted in those
namespaces. Admin can create/get/patch run CRs; operator owns Job lifecycle,
status and history deletion. See [database setup](database-setup.md),
[local deployment](local-deployment.md) and [usage](../USAGE.md).

<a id="accepted-correctness-work-in-progress"></a>

## Execution recovery and concurrent admission

The operator retains a missing bound Job as nonterminal `ExecutionUnknown`.
Its original Job/JobRun identity
and durable observations are the recovery authority. Missing resources do not
prove termination or authorize another attempt. Unknown steps block new
Workflow work while already running siblings remain observable. Explicit
cancellation requires retained termination evidence to establish `Canceled`.
New workload Pod templates include an Operator-owned evidence finalizer.
Cancellation shortens the original Job's deadline with UID/resourceVersion
checks and waits for a controller stop condition before collecting a complete
Pod inventory. A deletion timestamp cannot establish that in-flight Pod creation
has ended. Retained Pods preserve exit evidence during controller cleanup.
The Operator commits complete owned-container evidence before releasing its own
finalizer with Pod UID/resourceVersion preconditions. Success, failure, retries
and configured deadlines use the same persistence and release ordering before
ledger progression. Recovery includes pending releases on historical attempts;
retention cannot remove their ownership records first. Cancellation deletes the
original Job only after proof persistence and release, and confirms `Canceled`
after its removal and a final transactional proof check.

Canceling runs cannot acquire stop proof from a Job or attempt phase alone.
The five-minute confirmation window uses the persisted cancellation timestamp,
so a restart cannot extend it. Pure timeout remains uncertain and can recover
from a complete retained inventory. A definite evidence gap atomically revokes
proof and isolates the attempt under the parent/run/attempt lock order; a later
narrower inventory cannot clear that isolation. Legacy workloads without the
evidence finalizer keep conservative recovery semantics.

Client-go leadership sessions determine authority without comparing remote
wall-clock Lease expiry. After a leadership term is lost, its work must be canceled and drained before
the same process campaigns again. This coordinates leadership without providing
universal writer fencing.

Source admission and revision/lifecycle writes serialize under the same PostgreSQL lock in a
READ COMMITTED transaction. Replays return the original occurrence first.
Accepted manual run CRs blocked by Forbid remain pending without a new ledger
row; existing scheduled skip/misfire rules and Workflow scheduled rejection
receipts remain separate contracts. Unknown executions retain their slot.
The operator and PostgreSQL repositories implement these contracts. Leadership
coordinates singleton loops; it does not provide a universal fencing token
across PostgreSQL and Kubernetes. See [verification](verification.md) for the
covered failure and concurrency scenarios.

<a id="sources-of-truth"></a>

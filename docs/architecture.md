# Architecture

OrbitJob uses Kubernetes Jobs to execute container workloads, PostgreSQL to
record durable execution facts, and the Admin API to manage definitions and
access. The Operator controls execution. The scheduler evaluates SLOs from
recorded observations. These responsibilities remain separate during failures
and recovery.

## Components

| Component | Responsibility |
|---|---|
| Admin API | Authenticates callers, enforces policies, writes management state and its transactional audit, publishes/cancels run resources, and reads execution history. It cannot write the execution ledger. |
| Operator | Projects immutable revisions, admits runs, creates and observes Kubernetes Jobs, schedules jobs and Checks, synchronizes Functions, advances Workflows, recovers derived outcomes and prunes eligible history. |
| Scheduler | Evaluates SLI snapshots into SLO budgets and burn alerts. It does not create workload Jobs. |
| Definition admission | Validates final Job/Workflow definitions against checked-save identity before Kubernetes persistence, with explicit trusted native-writer exceptions. |
| Log archive | Stores bounded execution evidence on a dedicated PVC and maintains a tenant-scoped PostgreSQL identity directory. It admits collector output and serves internal readers over mTLS. |
| Fluent Bit collector | Reads mapped workload namespaces' node CRI output and forwards verified-source records to the archive. |
| Console | Serves the browser authoring, execution, monitoring and access-management interface under the Admin API's `/console/` origin. |
| CLI | Calls the HTTP API and uses kubectl for installation diagnostics; it has no database access. |

Installation commands generate database credentials (`configure`), apply ordered
checksummed migrations (`migrate`), deliver the bootstrap key (`bootstrap`) and
issue personal-account setup/recovery capabilities (`orbitjob-identity`). They
require installation authority and are separate from ordinary workload clients.
See [database setup](database-setup.md), [accounts](accounts.md) and
[local deployment](local-deployment.md).

## Definitions and execution

ScheduledJob and WorkflowJob are Kubernetes custom resources. Checks and
Functions are stored through the Admin API. The Operator projects these
configurations into immutable execution revisions. A JobRun represents a
logical occurrence; a WorkflowRun coordinates its step Runs. Each platform
Attempt binds to one Kubernetes Job. Kubernetes backoff can create multiple
Pods within that Attempt.

Manual job triggers and Function invocations publish JobRun resources;
Workflow triggers publish WorkflowRun resources. The response references the
resource and occurrence key, while ledger materialization follows asynchronously.
Scheduled occurrences and workflow steps record their durable identity before
publication. Reconciliation uses that stored identity after interruptions.

The platform has one execution backend, Kubernetes Jobs. There is no
in-process worker fallback. `timeoutSeconds` becomes a Kubernetes Job deadline
for each Attempt; it does not bound the combined wall-clock time of all
Attempts. [Execution model](concepts/execution-model.md) describes phases,
retry budgets, concurrency and cancellation from a workload user's perspective.

## Management and tenant boundaries

A browser session identifies a personal user; an API key identifies an automation
caller. Workspace membership and effective action/resource grants authorize
workload requests. Installation-administrator status authorizes global account
and workspace-metadata administration, with no implicit workload access.

The API resolves trusted tenant, group and actor identity from authentication
and the authorized target. Client JSON cannot replace those identities. Explicit
Deny takes precedence. Missing grants deny access. Policy-boundary intersection
and delegation containment fail closed when supported resource/action semantics
cannot establish containment.

Group-aware resources and evidence resolve their actual parent group. Routes
that cannot enforce group scope reject group-bound credentials. Platform callers
select one authorized target tenant for both authorization and data access;
a selector never grants cross-tenant access by itself. See
[manage access](tasks/manage-access.md) and [security](../SECURITY.md).

## Persistent state and database roles

PostgreSQL stores management state, immutable revisions, execution history,
projection acknowledgements, audit events and the log identity directory.

- `job_definition_revisions` stores executable snapshots.
- `job_run_control_plane` and `job_run_attempts_control_plane` store Run and
  Attempt facts; `workflow_run_control_plane` stores parent/step decisions.
- Checks, Functions and their derived history expose management/query models.
- SLI snapshots, SLO budgets and alerts record monitoring evidence.
- Audit changes commit in the same transaction as their domain change.
- `outcome_recorded_at` acknowledges required terminal projections;
  `sli_snapshot_events` deduplicates a tenant/run/SLI increment with its snapshot
  write. Deduplication facts outlive normal run pruning.

Application roles do not own tenant tables. Repositories set transaction-local
`app.tenant_id` before scoped queries, and RLS applies to those roles. The table
owner is NOLOGIN; the migration identity can assume it. Admin has management
writes and read-only execution history. Operator, scheduler, admission and archive use the Runtime
login with their required runtime grants. Narrow SECURITY DEFINER operations
cover identity resolution that must occur before a normal tenant transaction.

The log archive uses the trusted Runtime DB identity. Admin reads its directory
and commits audited cleanup requests; container output, manifests and lifecycle
events live on the archive PVC. The database and filesystem do not share a
transaction. [Execution logs](execution-logs.md) explains replay, capacity,
retention and coordinated backup.

## Creation and immutable identity

Before an external Job creation request, the Operator commits an empty-UID
`CreatingAttempt` intent. A subsequent transaction locks the Run/Attempt, checks
current state and performs bounded Job Ensure. An uncertain Kubernetes response
cannot prove that no Job was created. Recovery resolves the original address,
verifies complete identity and binds the confirmed UID.

Later observations must match that immutable identity and preserve monotonic
resourceVersion/phase processing. Names alone are insufficient. Current names
retain complete occurrence digests; historical short names remain bounded lookup
candidates only after complete identity checks. Mutation and retention use UID
and resourceVersion guards against replacement resources.

Source admission and revision/lifecycle writes serialize through PostgreSQL
locks in READ COMMITTED transactions. Replays resolve their original occurrence
before admitting new work. Manual JobRun requests blocked by Forbid remain
pending without a new ledger row. Scheduled skip/misfire handling and rejected
Workflow occurrence receipts are separate admission contracts. Unknown
executions retain their concurrency slot.

## Execution recovery and concurrent admission

A missing bound Job becomes nonterminal `ExecutionUnknown`. OrbitJob retains
its original identity and observations; missing resources do not authorize
another Attempt, a terminal outcome or release of a Forbid slot. Unknown
Workflow steps block new work while already running siblings remain observable.

Cancellation patches intent with UID/resourceVersion checks. The Operator
shortens the original Job deadline and requires controller stop acknowledgement
before accepting a complete owned-Pod inventory. A deletion timestamp cannot
establish that in-flight Pod creation ended. New workload Pods retain an
Operator-owned termination-evidence finalizer so exit observations can survive
controller cleanup.

The Operator commits complete owned-container evidence before releasing its
finalizer with Pod UID/resourceVersion preconditions. Success, failure, retry
and deadline handling preserve this persistence/release ordering before ledger
progression. Cancellation deletes the original Job only after proof persistence
and release, then checks proof transactionally before recording `Canceled`.
A stopped or terminal Workflow parent blocks new child reservation/creation.

The cancellation confirmation window uses the persisted request timestamp; a
restart does not extend it. A timeout preserves uncertainty and can recover from
complete retained evidence. A definite evidence gap revokes proof and isolates
the Attempt; a later narrower inventory cannot erase that gap. Legacy workloads
without the evidence hold retain conservative recovery semantics. Node loss or
external removal of evidence does not prove workload termination.

Client-go Lease leadership coordinates singleton schedule, Check, Function,
Workflow and retention loops. Lost leadership cancels and drains that term's
work before recampaigning. Lease sessions do not provide universal fencing
across PostgreSQL and Kubernetes. Transactional admission supplies the separate
source-level concurrency boundary. Idempotent terminal-projection replay runs
without requiring leadership.

## Terminal recovery and SLOs

Terminal execution facts commit before derived Check, Function and SLI writes.
Acknowledgement follows all required idempotent projections. Failed projections
remain pending and protected from pruning. Per-tenant keyset scanning allows
later healthy rows to progress despite earlier failures.

Migration 0012 preserves historical aggregates and acknowledges pre-cutover
terminal rows. It does not reconstruct unprovable historical counts. Old
operators must stop before that migration; see the
[database cutover procedure](database-setup.md#writer-cutover).

HTTP Checks execute digest-pinned probe containers. Confirmed outcomes produce
Check results and five-minute SLI buckets. The scheduler evaluates SLO windows
and burn alerts from those buckets. Criteria use produced status/duration facts;
schedule adherence has no dedicated SLI derivation.

## Browser and external interfaces

The Admin image builds and serves a static Console. Its browser requests use a
same-origin HttpOnly session cookie and an in-memory CSRF token. API keys remain
separate automation credentials. The public documentation site accepts no
installation credentials. Go-only API development can run without a frontend
bundle; that does not establish a usable installed Console.

Online definition routes accept typed Job/Workflow specs in mapped namespaces,
with independent Apply permission, dry-run admission and UID/resourceVersion
comparison. The default chart enables a fail-closed validating webhook that
checks the final admitted spec against the reviewed digest and live operation.
Explicit native writers still need a supported typed spec and bypass the
checked-save receipt requirement. They do not expose arbitrary Kubernetes writes. Log reads resolve
immutable ownership through retained history and require separate ReadLogs
permission. The Admin API exposes no generic Kubernetes proxy, Secret, exec or
attach interface.

The HTTP contract is [OpenAPI](../api/openapi.yaml), served at `/openapi.json`.
Structured logs, Prometheus metrics and `X-Trace-ID` support observation and
request correlation. Distributed tracing spans and an OpenTelemetry exporter
are not implemented. [Verification](verification.md) records scoped test and
installation evidence; [known issues](known-issues.md) records remaining limits.

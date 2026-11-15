# Project status

This page describes the backend capabilities in the current source. It does not
establish deployment or release acceptance. Release availability is recorded in the
[Changelog](../CHANGELOG.md); test results and their scope are maintained in
[verification](verification.md).

## Current capabilities

| Area | Implemented behavior |
|---|---|
| Execution | ScheduledJob cron and manual execution, immutable definition revisions, retries, deadlines, cancellation, run and attempt history |
| Workflows | WorkflowJob DAGs, automatic and manual starts, dependency conditions, failure policies and cancellation fan-out |
| Functions | Versioned definition management, invocation, active revision synchronization and invocation history |
| Reliability | Container-based HTTP checks, supported status/duration SLI criteria, SLO windows, error budgets and burn alerts |
| Administration | Personal accounts, workspace memberships, revocable HTTP sessions, API keys, explicit policy grants, resource groups and authorized workspace discovery |
| Personal accounts | Invite-only accounts, password authentication, revocable browser sessions, explicit workspace memberships and installation-administrator recovery |
| Execution logs | Bounded stdout/stderr and lifecycle evidence retained independently of Job, Pod and run-resource cleanup; authorized reading and audited deletion |
| Interfaces | Admin HTTP API, generated OpenAPI, Kubernetes CRDs and CLI |
| Delivery | Helm installation with an internal Admin Service, source builds and development-cluster tooling |
| Maintenance | Bounded tenant-scoped SLI replay-detail retirement, ledger-based execution-isolation inventory, and fault-recovery canaries with independent evidence checks |

The [architecture](architecture.md) defines component responsibilities and
trust boundaries. [Usage](../USAGE.md) describes supported operations;
[execution logs](execution-logs.md) defines retention and capacity behavior.

The [account guide](accounts.md) defines first-administrator setup, invitation,
password and session handling. Global password reset requires an installation
administrator; workspace administrators manage their own memberships and grants.
Installation-administrator status does not imply workspace workload access.

## Execution and isolation guarantees

Kubernetes runs containers; PostgreSQL records execution facts. The Admin API
writes management state and publishes Kubernetes intent. The Operator owns
execution-ledger updates. The Runtime database identity is trusted; application
policy, database RLS and namespace RBAC enforce separate access boundaries.

Durable creation intent and immutable Job identities protect recovery after
ambiguous external operations. Missing execution evidence remains nonterminal
`ExecutionUnknown`; it neither permits a replacement attempt nor frees a
`Forbid` admission slot. Cancellation completes only with sufficient owned
termination evidence. Terminal projections retry idempotently, and unacknowledged
outcomes remain protected from retention.

New workload Pods retain container exit evidence until it is persisted. The
Operator requires controller stop acknowledgement before accepting a complete
inventory, releases only its own evidence finalizer, then deletes the original
Job. Recovery and retention preserve unfinished evidence obligations, including
historical attempts. A deletion delay may recover after trustworthy proof;
a definite evidence gap remains isolated.

Operator singleton work uses client-go Lease sessions. Source admission uses
transaction-scoped PostgreSQL locks shared with revision and lifecycle updates.
Leadership coordinates work; it does not provide universal fencing across
PostgreSQL and Kubernetes. External workload effects are at least once, so
workloads must provide idempotency where repeated effects are unsafe.

## Compatibility and operations

Use the chart and environment examples for configuration, the migration files
for database schema, and OpenAPI for HTTP contracts. The
[deployment guide](local-deployment.md) and [database guide](database-setup.md)
define installation, credentials and migration procedures.

Personal accounts require migration 0027 and its matching Admin image.
Operators issue single-use setup or recovery links; ordinary users join through
workspace invitations and sign in with their own accounts. Remembered sessions
expire after seven days. Existing API keys retain their automation semantics and
do not become personal accounts. See [account setup and access](accounts.md).

The backend Admin image contains the account API and identity tool without a
compiled Console bundle. `/console/` returns 503 when assets are absent; API
routes remain available. The chart exposes Admin through an internal ClusterIP
Service on port 8080. Administrators provide any external HTTPS proxy, routing
and certificates. See [installation access](../USAGE.md#72-first-install).

Older execution writers must stop before the recovery and resource-identity
migration cutover. Preserve recorded migration checksums and the installation's
migration input channel. The database and log archive require durable storage;
resource cleanup must preserve their data unless deletion is explicitly intended.
The personal-account candidate adds migration 0027. Its installed cutover and
the existing development cluster's network/storage maintenance follow the
[maintenance procedure](local-maintenance.md); repository implementation does not
establish completion of that window.

## Remaining limitations

Unknown execution can retain a concurrency slot indefinitely when termination
cannot be proven. SLI retirement preserves incomplete plans and queryable missing
evidence; it covers tenants still assigned to the Operator. Missing timestamps can
prevent automatic retirement.

Private authoring drafts currently have migration 0026 and service primitives
for validation, encryption and recovery. A persistent Store adapter and HTTP
routes are not implemented, so users cannot save or recover drafts through the
API. Schedule-adherence SLI derivation, general workflow dataflow and cross-cluster
scheduling are outside the implemented scope.

The documented installation profile and controlled recovery tests do not define
production throughput or HA qualification. See [known issues](known-issues.md)
for operational constraints and [verification](verification.md) for the tested
behaviors, environments and outstanding evidence.
